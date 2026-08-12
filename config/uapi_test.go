package config

import (
	"net/netip"
	"strings"
	"testing"
)

func TestUAPI(t *testing.T) {
	cfg := &Config{
		PrivateKey: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		Peer: Peer{
			PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			AllowedIPs: []netip.Prefix{
				netip.MustParsePrefix("0.0.0.0/0"),
				netip.MustParsePrefix("::/0"),
			},
			Keepalive: 25,
		},
	}

	got, err := cfg.UAPI(netip.MustParseAddrPort("192.0.2.10:51820"))
	if err != nil {
		t.Fatalf("UAPI: %v", err)
	}

	want := strings.Join([]string{
		"private_key=0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		"public_key=0000000000000000000000000000000000000000000000000000000000000000",
		"endpoint=192.0.2.10:51820",
		"allowed_ip=0.0.0.0/0",
		"allowed_ip=::/0",
		"persistent_keepalive_interval=25",
		"",
	}, "\n")
	if got != want {
		t.Errorf("UAPI mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestUAPIPresharedAndNoKeepalive(t *testing.T) {
	cfg := &Config{
		PrivateKey: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
		Peer: Peer{
			PublicKey:    "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			PresharedKey: "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
			AllowedIPs:   []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
		},
	}

	got, err := cfg.UAPI(netip.MustParseAddrPort("192.0.2.10:51820"))
	if err != nil {
		t.Fatalf("UAPI: %v", err)
	}

	if !strings.Contains(got, "preshared_key=0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20\n") {
		t.Error("preshared_key line missing")
	}
	if strings.Contains(got, "persistent_keepalive_interval") {
		t.Error("keepalive line present despite Keepalive = 0")
	}
}

func TestUAPIBadKey(t *testing.T) {
	cfg := &Config{
		PrivateKey: "not a key",
		Peer:       Peer{PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="},
	}
	if _, err := cfg.UAPI(netip.MustParseAddrPort("192.0.2.10:51820")); err == nil {
		t.Fatal("expected an error for an invalid key")
	}
}
