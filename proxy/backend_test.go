package proxy

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	xproxy "golang.org/x/net/proxy"
)

func spy(name string, hits *[]string, inner Backend) Backend {
	return Backend{
		Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			*hits = append(*hits, name)
			return inner.Dial(ctx, network, addr)
		},
		Resolve: inner.Resolve,
	}
}

// TestSetBackendSwitchesRouting proves a live server can be repointed:
// connections made before and after the switch land on different
// backends.
func TestSetBackendSwitchesRouting(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "direct world")
	}))
	defer target.Close()

	var hits []string
	s := New(spy("first", &hits, DirectBackend()), log)
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Serve() }()

	client := socksClient(t, s.Addr().String())
	mustGet(t, client, target.URL)

	s.SetBackend(spy("second", &hits, DirectBackend()))
	// Drop the pooled keep-alive connection: streams keep the backend
	// they started with, so only a fresh dial can see the new one.
	client.Transport.(*http.Transport).CloseIdleConnections()
	mustGet(t, client, target.URL)

	if want := "first,second"; strings.Join(hits, ",") != want {
		t.Errorf("backend hits = %v, want %s", hits, want)
	}
}

// TestDirectBackendServesWithoutTunnel is the fallback mode contract:
// no tunnel exists anywhere, yet requests through the proxy succeed via
// the operating system's own networking.
func TestDirectBackendServesWithoutTunnel(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "direct world")
	}))
	defer target.Close()

	s := New(DirectBackend(), log)
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	go func() { _ = s.Serve() }()

	body := mustGet(t, socksClient(t, s.Addr().String()), target.URL)
	if body != "direct world" {
		t.Errorf("body = %q", body)
	}
}

func socksClient(t *testing.T, addr string) *http.Client {
	t.Helper()
	dialer, err := xproxy.SOCKS5("tcp", addr, nil, xproxy.Direct)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Transport: &http.Transport{DialContext: dialer.(xproxy.ContextDialer).DialContext},
		Timeout:   10 * time.Second,
	}
}

func mustGet(t *testing.T, client *http.Client, url string) string {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, err %v", resp.StatusCode, err)
	}
	return string(b)
}
