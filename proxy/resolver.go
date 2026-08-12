package proxy

import (
	"context"
	"fmt"
	"net"
)

// hostLookup is the one netstack capability the resolver needs; an
// interface keeps it fakeable in tests.
type hostLookup interface {
	LookupContextHost(ctx context.Context, host string) ([]string, error)
}

// resolver answers SOCKS hostname requests inside the tunnel. Without it
// the library falls back to the OS resolver and internal names leak to
// the local DNS, which is exactly what proxied DNS exists to prevent.
type resolver struct {
	tnet hostLookup
}

func (r resolver) Resolve(ctx context.Context, name string) (context.Context, net.IP, error) {
	addrs, err := r.tnet.LookupContextHost(ctx, name)
	if err != nil {
		return ctx, nil, fmt.Errorf("resolve %s: %w", name, err)
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil {
			return ctx, ip, nil
		}
	}
	return ctx, nil, fmt.Errorf("resolve %s: no usable address", name)
}
