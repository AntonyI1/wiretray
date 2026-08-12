package engine

import (
	"context"
	"time"
)

const (
	watchInterval = 30 * time.Second
	staleAfter    = 3 * time.Minute
)

// Watch self-heals a running tunnel and blocks until ctx ends. When
// handshakes stall it re-resolves the endpoint and reapplies it, which
// covers the VPN server changing address and the machine waking from
// sleep; WireGuard's own keepalive then re-handshakes.
func (t *Tunnel) Watch(ctx context.Context) {
	tick := time.NewTicker(watchInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}

		st, err := t.Status()
		if err != nil {
			continue // device is gone; the context ends shortly after
		}
		if !stalled(time.Now(), st.LastHandshake) {
			continue
		}

		t.log.Warn("handshake stalled, refreshing endpoint",
			"age", time.Since(st.LastHandshake).Round(time.Second))
		if err := t.refreshEndpoint(); err != nil {
			t.log.Error("refresh endpoint: " + err.Error())
		}
	}
}

// stalled reports whether a live tunnel's handshake has gone quiet. A
// tunnel that never handshook is not stalled; the connecting timeout
// owns that case.
func stalled(now, lastHandshake time.Time) bool {
	if lastHandshake.IsZero() {
		return false
	}
	return now.Sub(lastHandshake) > staleAfter
}

func (t *Tunnel) refreshEndpoint() error {
	endpoint, err := resolveEndpoint(t.cfg.Peer.Endpoint)
	if err != nil {
		return err
	}
	frag, err := t.cfg.EndpointUAPI(endpoint)
	if err != nil {
		return err
	}
	return t.dev.IpcSet(frag)
}
