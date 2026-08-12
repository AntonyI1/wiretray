package engine_test

import (
	"io"
	"log/slog"
	"net/netip"
	"runtime"
	"testing"
	"time"

	"github.com/AntonyI1/wiretray/config"
	"github.com/AntonyI1/wiretray/engine"
)

// TestRepeatedStartStop proves toggling leaks nothing: goroutine count
// settles back to its post-first-cycle baseline after ten cycles. The
// endpoint points at a dead loopback port on purpose; lifecycle does not
// need a live peer.
func TestRepeatedStartStop(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		PrivateKey: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		Address:    netip.MustParseAddr("10.0.0.2"),
		MTU:        1420,
		Peer: config.Peer{
			PublicKey:  "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			Endpoint:   "127.0.0.1:40404",
			AllowedIPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")},
		},
	}

	settle := func() int {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
		return runtime.NumGoroutine()
	}

	var baseline int
	for i := 0; i < 10; i++ {
		tn, err := engine.Start(cfg, log)
		if err != nil {
			t.Fatalf("cycle %d Start: %v", i, err)
		}
		tn.Stop()
		if i == 0 {
			baseline = settle()
		}
	}

	// Teardown is asynchronous; give it a bounded window to converge.
	deadline := time.Now().Add(5 * time.Second)
	final := settle()
	for final > baseline+3 && time.Now().Before(deadline) {
		final = settle()
	}
	if final > baseline+3 {
		t.Errorf("goroutines grew from %d to %d over 10 cycles", baseline, final)
	}
}
