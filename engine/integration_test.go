package engine_test

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"

	"github.com/AntonyI1/wiretray/config"
	"github.com/AntonyI1/wiretray/engine"
	"github.com/AntonyI1/wiretray/proxy"
)

const (
	serverAddr = "10.0.0.1"
	clientAddr = "10.0.0.2"
	serverPort = 51999 // fixed loopback UDP port for the test peer
	body       = "hello from inside the tunnel"
)

// TestEndToEnd stands up a second WireGuard peer inside the test binary
// and drives an HTTP request through the real SOCKS server, the real
// netstack, and a real Noise handshake over loopback UDP. No root, no
// network, no infrastructure.
func TestEndToEnd(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	serverKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	startServerPeer(t, serverKey, clientKey)

	cfg := testClientConfig(clientKey, serverKey)

	tn, err := engine.Start(cfg, log)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tn.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tn.AwaitHandshake(ctx); err != nil {
		t.Fatalf("AwaitHandshake: %v", err)
	}

	st, err := tn.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.LastHandshake.IsZero() {
		t.Fatal("handshake reported done but LastHandshake is zero")
	}

	srv := proxy.New(tn.Net(), log)
	if err := srv.Listen(cfg.Bind); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer srv.Close()
	go func() {
		if err := srv.Serve(); err != nil {
			t.Errorf("Serve: %v", err)
		}
	}()

	dialer, err := xproxy.SOCKS5("tcp", srv.Addr().String(), nil, xproxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: &http.Transport{DialContext: dialer.(xproxy.ContextDialer).DialContext},
		Timeout:   10 * time.Second,
	}

	resp, err := client.Get(fmt.Sprintf("http://%s/", serverAddr))
	if err != nil {
		t.Fatalf("GET through proxy: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(got) != body {
		t.Fatalf("got %d %q, want 200 %q", resp.StatusCode, got, body)
	}

	if err := srv.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	tn.Stop()
	tn.Stop() // stopping twice must be safe
}

// startServerPeer brings up the far side of the tunnel: a raw
// wireguard-go device on its own netstack with an HTTP server inside.
// It serves the hello body at / and blobSize zero bytes at /blob for
// the throughput benchmark.
func startServerPeer(t testing.TB, serverKey, clientKey *ecdh.PrivateKey) {
	t.Helper()

	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{netip.MustParseAddr(serverAddr)}, nil, 1420)
	if err != nil {
		t.Fatal(err)
	}

	dev := device.NewDevice(tun, conn.NewDefaultBind(),
		device.NewLogger(device.LogLevelError, "server: "))
	uapi := fmt.Sprintf(
		"private_key=%x\nlisten_port=%d\npublic_key=%x\nallowed_ip=%s/32\n",
		serverKey.Bytes(), serverPort, clientKey.PublicKey().Bytes(), clientAddr)
	if err := dev.IpcSet(uapi); err != nil {
		t.Fatalf("server IpcSet: %v", err)
	}
	if err := dev.Up(); err != nil {
		t.Fatalf("server Up: %v", err)
	}
	t.Cleanup(dev.Close)

	ln, err := tnet.ListenTCPAddrPort(netip.AddrPortFrom(netip.MustParseAddr(serverAddr), 80))
	if err != nil {
		t.Fatalf("listen inside server netstack: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, body)
	})
	mux.HandleFunc("/blob", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(blob)
	})
	go func() { _ = http.Serve(ln, mux) }()
}

// testClientConfig is the client side of the test pair, pointed at the
// in-process server peer.
func testClientConfig(clientKey, serverKey *ecdh.PrivateKey) *config.Config {
	return &config.Config{
		PrivateKey: base64.StdEncoding.EncodeToString(clientKey.Bytes()),
		Address:    netip.MustParseAddr(clientAddr),
		MTU:        1420,
		Bind:       "127.0.0.1:0",
		Peer: config.Peer{
			PublicKey:  base64.StdEncoding.EncodeToString(serverKey.PublicKey().Bytes()),
			Endpoint:   fmt.Sprintf("127.0.0.1:%d", serverPort),
			AllowedIPs: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")},
			Keepalive:  25,
		},
	}
}
