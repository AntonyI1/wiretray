package engine

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/AntonyI1/wiretray/config"
)

// Tunnel is a running userspace WireGuard instance. It owns no routes
// and no network interfaces; it exists only as this process's memory,
// which is the whole point.
type Tunnel struct {
	dev  *device.Device
	tnet *netstack.Net
	log  *slog.Logger

	stopOnce sync.Once
}

// Start brings the device up and returns without waiting for the peer;
// use AwaitHandshake to block until the tunnel is actually live.
func Start(cfg *config.Config, log *slog.Logger) (*Tunnel, error) {
	// The endpoint resolves on the system resolver by design: the
	// encrypted outer packets must travel over normal networking.
	ua, err := net.ResolveUDPAddr("udp", cfg.Peer.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("resolve endpoint %s: %w", cfg.Peer.Endpoint, err)
	}
	endpoint := netip.AddrPortFrom(ua.AddrPort().Addr().Unmap(), ua.AddrPort().Port())

	uapi, err := cfg.UAPI(endpoint)
	if err != nil {
		return nil, err
	}

	tun, tnet, err := netstack.CreateNetTUN([]netip.Addr{cfg.Address}, cfg.DNS, cfg.MTU)
	if err != nil {
		return nil, fmt.Errorf("create netstack: %w", err)
	}

	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelError, "wg: "))
	if err := dev.IpcSet(uapi); err != nil {
		dev.Close()
		return nil, fmt.Errorf("configure device: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("bring device up: %w", err)
	}

	log.Info("tunnel up", "endpoint", endpoint.String(), "address", cfg.Address.String())
	return &Tunnel{dev: dev, tnet: tnet, log: log}, nil
}

// Net exposes the tunnel's network stack. Connections dialed through it
// travel inside the tunnel.
func (t *Tunnel) Net() *netstack.Net { return t.tnet }

// AwaitHandshake blocks until the peer completes a handshake or ctx
// gives up. A tunnel that never handshakes usually means wrong keys,
// wrong endpoint, or blocked UDP.
func (t *Tunnel) AwaitHandshake(ctx context.Context) error {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		st, err := t.Status()
		if err == nil && !st.LastHandshake.IsZero() {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("no handshake: check keys, endpoint, UDP reachability: %w", ctx.Err())
		case <-tick.C:
		}
	}
}

// Stop tears the tunnel down. Safe to call more than once; Close also
// brings the device down first.
func (t *Tunnel) Stop() {
	t.stopOnce.Do(func() {
		t.dev.Close()
		t.log.Info("tunnel stopped")
	})
}
