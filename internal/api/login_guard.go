package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ashokhin/am4bot/internal/store"
)

// maxFailedLoginAttempts/loginBanDuration: 5 consecutive failures bans for
// an hour -- see LoginGuard's doc comment for the three separate cases
// this covers.
const (
	maxFailedLoginAttempts = 5
	loginBanDuration       = time.Hour
	// maxIPTotalFailedAttempts bans an IP outright once its TOTAL failure
	// count (any login, repeats included) crosses this, even if it never
	// grew past a couple of distinct logins. 2x maxFailedLoginAttempts,
	// not an arbitrary round number: an attacker alternating between just
	// two known accounts (e.g. "admin" and one known user's login) hits
	// each one's own 5-failure cap at very nearly the same time, for 10
	// total failures right as both individual logins ban themselves --
	// this threshold trips at that same moment, so the IP itself is ALSO
	// blocked and can't immediately pivot to a third account the moment
	// those two per-login bans are up. A higher threshold (e.g. 20) would
	// never fire in this exact scenario, since both logins self-throttle
	// at 5 real failures each before Check() starts refusing further
	// attempts against them without ever reaching RecordFailure again.
	maxIPTotalFailedAttempts = 2 * maxFailedLoginAttempts
)

const (
	banKeyTypeIP    = "ip"
	banKeyTypeLogin = "login"
)

// LoginGuard rate-limits POST /api/auth/login with THREE deliberately
// separate mechanisms, each catching a different attack shape and never
// triggering the other two on its own:
//
//   - Password guessing (many wrong passwords against ONE login): counts
//     consecutive failures against that login, regardless of source IP,
//     and bans the LOGIN after maxFailedLoginAttempts. This is also what
//     happens when a real person just mistypes their own password
//     repeatedly -- see handleUnlockUser for how an admin lifts it.
//   - Login guessing / enumeration (one IP trying many DIFFERENT logins):
//     counts the number of DISTINCT logins that have failed from one IP,
//     and bans the IP once that count reaches maxFailedLoginAttempts.
//     Repeatedly failing the SAME login from one IP never grows this past
//     1 -- that's the password-guessing case above, handled entirely by
//     the login-side counter, and must NOT also ban the IP (early
//     versions of this did, which meant one person testing their own
//     wrong password locked themselves out of the admin UI too, with no
//     other way back in).
//   - Sustained hammering of a SMALL, FIXED set of known logins (e.g.
//     alternating "admin" and one known user's login back and forth,
//     never touching a 3rd, 4th, 5th...): the distinct-login case above
//     never fires (never more than a couple of distinct logins), and each
//     individual login only self-throttles at maxFailedLoginAttempts.
//     maxIPTotalFailedAttempts catches this by counting EVERY failure
//     from an IP, repeats included, and banning the IP once that raw
//     total crosses it -- see that constant's own doc comment for why
//     its value is derived from maxFailedLoginAttempts rather than
//     picked arbitrarily.
//
// Active bans live in Postgres (store.LoginBan), so a restart doesn't
// quietly forgive an in-progress attack, and an admin can see/lift a
// login-type ban from the user detail page (see handleUnlockUser). Every
// ban creation and manual unlock is additionally appended to
// login_ban_events, a permanent audit trail store.LoginBan itself doesn't
// keep (it only holds currently-active bans).
//
// The in-memory counters (loginFailures, ipFailedLogins) are NOT
// persisted -- only a ban itself is. A restart mid-attack resets an
// in-progress (not yet banned) count back to zero; acceptable at this
// project's scale, where apiserver restarts are rare and this exists to
// stop scripted brute-forcing, not to survive an adversarial
// restart-timing attack.
//
// Client IP is taken from r.RemoteAddr only, never an X-Forwarded-For
// header -- that header is trivially spoofable by the caller unless a
// reverse proxy is configured to strip/overwrite it, which this package
// has no way to verify. Deployed behind a reverse proxy, every request
// apiserver sees will carry the proxy's own address here, so IP-based
// banning effectively becomes "ban everyone behind this proxy after 5
// logins failed from anywhere" -- acceptable for this project's
// single-reverse-proxy, handful-of-users deployment shape, but worth
// revisiting (trusted-proxy allowlist + XFF) before relying on this at a
// larger scale.
type LoginGuard struct {
	store *store.Store

	mu sync.Mutex
	// loginFailures[login] is that login's consecutive failure count.
	loginFailures map[string]int
	// ipFailedLogins[ip] is the SET of distinct logins that have failed
	// from that IP -- its size, not a raw failure count, is what's
	// compared against maxFailedLoginAttempts. A map[string]struct{}, not
	// a slice, so retrying the same login from the same IP is a no-op.
	ipFailedLogins map[string]map[string]struct{}
	// ipTotalFailures[ip] is that IP's RAW failure count, repeats
	// included (unlike ipFailedLogins's distinct-login set) -- compared
	// against maxIPTotalFailedAttempts. See that constant's doc comment.
	ipTotalFailures map[string]int
}

// NewLoginGuard creates a LoginGuard backed by st. Active bans are read
// from Postgres on demand (Check), not cached at startup -- there's no
// separate load step, unlike the earlier JSON-file design this replaced.
func NewLoginGuard(st *store.Store) *LoginGuard {
	return &LoginGuard{
		store:           st,
		loginFailures:   make(map[string]int),
		ipFailedLogins:  make(map[string]map[string]struct{}),
		ipTotalFailures: make(map[string]int),
	}
}

// Check returns the active ban blocking this attempt, if any. Called
// before the database user lookup/bcrypt are touched, so a banned caller
// doesn't get to spend the server's own CPU on a password it'll reject
// anyway. A store error is treated as "not banned" (fail open) and
// logged -- a database hiccup must never itself lock every login out.
func (g *LoginGuard) Check(ctx context.Context, ip, login string) (store.LoginBan, bool) {
	if b, err := g.store.GetActiveLoginBan(ctx, banKeyTypeIP, ip); err == nil {
		return *b, true
	} else if !isNotFound(err) {
		slog.Warn("checking ip login ban", "ip", ip, "error", err)
	}

	if login == "" {
		return store.LoginBan{}, false
	}

	if b, err := g.store.GetActiveLoginBan(ctx, banKeyTypeLogin, login); err == nil {
		return *b, true
	} else if !isNotFound(err) {
		slog.Warn("checking login ban", "login", login, "error", err)
	}

	return store.LoginBan{}, false
}

// RecordFailure records one more failed attempt: bumps the LOGIN's own
// consecutive-failure count (password-guessing case), and separately
// bumps the IP's distinct-failed-logins set (login-guessing case) --
// see LoginGuard's doc comment for why these never trigger each other.
func (g *LoginGuard) RecordFailure(ctx context.Context, ip, login string) {
	if login != "" {
		g.bumpLogin(ctx, login)
	}

	g.bumpIP(ctx, ip, login)
}

// bumpLogin increments login's consecutive failure count and, once it
// reaches the threshold, bans that LOGIN (password-guessing case).
func (g *LoginGuard) bumpLogin(ctx context.Context, login string) {
	g.mu.Lock()
	g.loginFailures[login]++
	count := g.loginFailures[login]

	if count < maxFailedLoginAttempts {
		g.mu.Unlock()

		return
	}

	g.loginFailures[login] = 0
	g.mu.Unlock()

	g.createBan(ctx, banKeyTypeLogin, login,
		fmt.Sprintf("%d consecutive failed login attempts against this login", maxFailedLoginAttempts))
}

// bumpIP updates both of an IP's counters -- the distinct-logins set
// (login-guessing case) and the raw total (sustained-hammering-of-a-few-
// known-logins case) -- and bans the IP the moment either threshold is
// reached. Retrying the same login from the same IP is a no-op for the
// distinct-logins set but still counts toward the raw total -- see
// LoginGuard's doc comment for why both exist.
func (g *LoginGuard) bumpIP(ctx context.Context, ip, login string) {
	g.mu.Lock()

	set, ok := g.ipFailedLogins[ip]
	if !ok {
		set = make(map[string]struct{})
		g.ipFailedLogins[ip] = set
	}

	set[login] = struct{}{}
	distinctCount := len(set)

	g.ipTotalFailures[ip]++
	totalCount := g.ipTotalFailures[ip]

	var reason string

	switch {
	case distinctCount >= maxFailedLoginAttempts:
		reason = fmt.Sprintf("failed login attempts against %d different logins from this IP", maxFailedLoginAttempts)
	case totalCount >= maxIPTotalFailedAttempts:
		reason = fmt.Sprintf("%d total failed login attempts from this IP", maxIPTotalFailedAttempts)
	default:
		g.mu.Unlock()

		return
	}

	delete(g.ipFailedLogins, ip)
	delete(g.ipTotalFailures, ip)
	g.mu.Unlock()

	g.createBan(ctx, banKeyTypeIP, ip, reason)
}

// createBan persists a new ban (both the current-state row and a
// permanent audit event) for (keyType, key).
func (g *LoginGuard) createBan(ctx context.Context, keyType, key, reason string) {
	bannedAt := time.Now()
	unbanAt := bannedAt.Add(loginBanDuration)

	if err := g.store.UpsertLoginBan(ctx, keyType, key, reason, unbanAt); err != nil {
		slog.Error("persisting login ban", "key_type", keyType, "key", key, "error", err)

		return
	}

	if err := g.store.RecordLoginBanEvent(ctx, keyType, key, reason, bannedAt, unbanAt); err != nil {
		slog.Error("recording login ban event", "key_type", keyType, "key", key, "error", err)
	}

	slog.Warn("login ban created", "key_type", keyType, "key", key, "reason", reason, "unban_at", unbanAt)
}

// RecordSuccess clears login's failure count and both of ip's counters --
// a successful login resets all three so earlier typos/lookups don't
// linger toward tripping a later ban. It does not lift an already-active
// ban: Check already refuses the attempt before credentials are verified,
// so this path isn't reachable while banned.
func (g *LoginGuard) RecordSuccess(ip, login string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	delete(g.loginFailures, login)
	delete(g.ipFailedLogins, ip)
	delete(g.ipTotalFailures, ip)
}

// UnlockLogin lifts an active login-type ban early (admin action -- see
// handleUnlockUser), and, if lastFailedIP is non-empty, ALSO lifts any
// active IP-type ban on that address. The two no longer trip together in
// the common case (see LoginGuard's doc comment -- repeatedly failing the
// SAME login from one IP only ever bans the login, never the IP), but a
// mixed incident (e.g. a shared office IP where one person is guessing
// logins while another just mistypes their own password) can still leave
// both banned independently; lastFailedIP (the account's own
// store.User.LastFailedLoginIP) covers that case too. Each lifted ban is
// recorded in login_ban_events. A no-op (not an error) for whichever of
// the two isn't currently banned.
func (g *LoginGuard) UnlockLogin(ctx context.Context, login, lastFailedIP, unlockedByLogin string) error {
	if err := g.unlock(ctx, banKeyTypeLogin, login, unlockedByLogin); err != nil {
		return err
	}

	if lastFailedIP != "" {
		if err := g.unlock(ctx, banKeyTypeIP, lastFailedIP, unlockedByLogin); err != nil {
			return err
		}
	}

	return nil
}

// unlock lifts one (keyType, key) ban -- see UnlockLogin, its only caller.
func (g *LoginGuard) unlock(ctx context.Context, keyType, key, unlockedByLogin string) error {
	if err := g.store.DeleteLoginBan(ctx, keyType, key); err != nil {
		return fmt.Errorf("deleting %s ban: %w", keyType, err)
	}

	if err := g.store.MarkLoginBanEventUnlocked(ctx, keyType, key, unlockedByLogin); err != nil {
		return fmt.Errorf("recording %s unlock event: %w", keyType, err)
	}

	g.mu.Lock()
	if keyType == banKeyTypeLogin {
		delete(g.loginFailures, key)
	} else {
		delete(g.ipFailedLogins, key)
		delete(g.ipTotalFailures, key)
	}
	g.mu.Unlock()

	return nil
}

func isNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}
