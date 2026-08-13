package engine_test

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestSoak holds a live two-peer tunnel under continuous light load and
// asserts nothing degrades: every request succeeds, the handshake stays
// fresh, and goroutines stay flat. Opt-in because it runs for minutes:
//
//	WIRETRAY_SOAK=1 go test ./engine -run TestSoak -v -timeout 20m
//
// WIRETRAY_SOAK_MINUTES overrides the default 10 minute duration.
func TestSoak(t *testing.T) {
	if os.Getenv("WIRETRAY_SOAK") == "" {
		t.Skip("set WIRETRAY_SOAK=1 to run the soak")
	}
	dur := 10 * time.Minute
	if v := os.Getenv("WIRETRAY_SOAK_MINUTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("WIRETRAY_SOAK_MINUTES=%q: %v", v, err)
		}
		dur = time.Duration(n) * time.Minute
	}

	tn, client := pairUp(t)

	var (
		requests, failures int
		maxHandshakeAge    time.Duration
		maxGoroutines      int
	)
	baseGoroutines := runtime.NumGoroutine()

	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://%s/", serverAddr))
		if err != nil {
			failures++
			t.Logf("request failed: %v", err)
		} else {
			if _, err := io.Copy(io.Discard, resp.Body); err != nil || resp.StatusCode != http.StatusOK {
				failures++
				t.Logf("bad response: status %d, err %v", resp.StatusCode, err)
			} else {
				requests++
			}
			resp.Body.Close()
		}

		st, err := tn.Status()
		if err != nil {
			t.Fatalf("Status: %v", err)
		}
		if age := time.Since(st.LastHandshake); age > maxHandshakeAge {
			maxHandshakeAge = age
		}
		if n := runtime.NumGoroutine(); n > maxGoroutines {
			maxGoroutines = n
		}

		time.Sleep(5 * time.Second)
	}

	rss := 0.0
	if v, ok := selfRSS(); ok {
		rss = v
	}
	t.Logf("soak %s: %d requests, %d failures, max handshake age %s, goroutines base %d max %d final %d, rss %.0fMB",
		dur, requests, failures, maxHandshakeAge.Round(time.Second),
		baseGoroutines, maxGoroutines, runtime.NumGoroutine(), rss)

	if failures > 0 {
		t.Errorf("%d of %d requests failed", failures, failures+requests)
	}
	// WireGuard rekeys roughly every two minutes under traffic; an age
	// beyond three minutes means liveness was lost at some point.
	if maxHandshakeAge > 3*time.Minute {
		t.Errorf("handshake went stale: max age %s", maxHandshakeAge)
	}
	if final := runtime.NumGoroutine(); final > baseGoroutines+10 {
		t.Errorf("goroutines grew from %d to %d", baseGoroutines, final)
	}
}
