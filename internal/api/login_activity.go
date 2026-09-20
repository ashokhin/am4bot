package api

import (
	"time"

	"github.com/ashokhin/am4bot/internal/store"
)

// loginActivityLimit is how many recent login attempts (success or
// failure) are surfaced in the UI -- on a user's own profile page and on
// the admin user detail page alike. The full history keeps accumulating
// in login_attempts regardless; this only bounds what's shown.
const loginActivityLimit = 10

// loginActivityEntry is one row of a user's recent login history, as
// shown in the UI.
type loginActivityEntry struct {
	Success   bool      `json:"success"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

func toLoginActivityEntries(attempts []store.LoginAttempt) []loginActivityEntry {
	out := make([]loginActivityEntry, len(attempts))
	for i, a := range attempts {
		out[i] = loginActivityEntry{
			Success:   a.Success,
			IP:        a.IP,
			UserAgent: a.UserAgent,
			CreatedAt: a.CreatedAt,
		}
	}

	return out
}
