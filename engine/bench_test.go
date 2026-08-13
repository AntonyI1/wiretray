package engine_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"

	"github.com/AntonyI1/wiretray/engine"
	"github.com/AntonyI1/wiretray/proxy"
)

const blobSize = 16 << 20

var blob = make([]byte, blobSize)

// pairUp stands up the full stack once: server peer, live tunnel,
// SOCKS proxy, and an HTTP client that dials through it.
func pairUp(t testing.TB) (*engine.Tunnel, *http.Client) {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	port := startServerPeer(t, serverKey, clientKey)

	tn, err := engine.Start(testClientConfig(clientKey, serverKey, port), log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tn.Stop)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tn.AwaitHandshake(ctx); err != nil {
		t.Fatal(err)
	}

	srv := proxy.New(tn.Net(), log)
	if err := srv.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	go func() { _ = srv.Serve() }()

	dialer, err := xproxy.SOCKS5("tcp", srv.Addr().String(), nil, xproxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: dialer.(xproxy.ContextDialer).DialContext},
		Timeout:   30 * time.Second,
	}
	return tn, client
}

// BenchmarkThroughput streams 16MB downloads through the entire path:
// SOCKS server, netstack TCP, WireGuard encryption, loopback UDP, and
// back up the far side's stack. The MB/s column is the headline number.
func BenchmarkThroughput(b *testing.B) {
	_, client := pairUp(b)

	b.SetBytes(blobSize)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resp, err := client.Get(fmt.Sprintf("http://%s/blob", serverAddr))
		if err != nil {
			b.Fatal(err)
		}
		n, err := io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err != nil || n != blobSize {
			b.Fatalf("read %d bytes, err %v", n, err)
		}
	}
	b.StopTimer()

	if rss, ok := selfRSS(); ok {
		b.ReportMetric(rss, "rssMB")
	}
}

// BenchmarkConnect measures toggle-to-live: engine start through the
// first completed handshake, torn down outside the timer.
func BenchmarkConnect(b *testing.B) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		b.Fatal(err)
	}
	port := startServerPeer(b, serverKey, clientKey)
	cfg := testClientConfig(clientKey, serverKey, port)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tn, err := engine.Start(cfg, log)
		if err != nil {
			b.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = tn.AwaitHandshake(ctx)
		cancel()
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		tn.Stop()
		b.StartTimer()
	}
}

// selfRSS reads this process's resident set from /proc, so it reports
// on Linux only. The number covers BOTH peers plus the test harness;
// the real app's footprint is roughly half.
func selfRSS() (float64, bool) {
	raw, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		v, ok := strings.CutPrefix(line, "VmRSS:")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) < 1 {
			return 0, false
		}
		kb, err := strconv.ParseFloat(fields[0], 64)
		if err != nil {
			return 0, false
		}
		return kb / 1024, true
	}
	return 0, false
}
