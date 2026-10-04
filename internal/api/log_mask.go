package api

import (
	"fmt"
	"net/netip"
)

// maskIP returns ip with its host part hidden, for the process log: an IPv4
// address keeps its /24 ("203.0.113.x"), an IPv6 address its /48
// ("2001:db8:1::/48"), and anything that isn't an IP address becomes
// "invalid". A full client address is personal data that a log file, shipped
// and retained separately from the database, has no need to carry; where the
// full address is actually needed (a user's login activity, the login-ban
// table) it is stored there instead. The result is rebuilt from the parsed
// address rather than cut from the input, so nothing beyond the masked
// prefix - or any non-address text - can reach the log through it.
func maskIP(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "invalid"
	}

	addr = addr.Unmap()

	if addr.Is4() {
		b := addr.As4()

		return fmt.Sprintf("%d.%d.%d.x", b[0], b[1], b[2])
	}

	prefix, err := addr.Prefix(48)
	if err != nil {
		return "invalid"
	}

	return prefix.String()
}
