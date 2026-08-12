package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// parseAddr reads an IP address that may carry a CIDR suffix, the way
// wg-quick files write them ("10.0.0.2" or "10.0.0.2/32"). The suffix
// is dropped; only the address matters to the netstack.
func parseAddr(s string) (netip.Addr, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Addr{}, fmt.Errorf("parse address %q: %w", s, err)
		}
		return p.Addr(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse address %q: %w", s, err)
	}
	return a, nil
}
