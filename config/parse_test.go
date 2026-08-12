package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeConf drops config text into a throwaway file and returns its path.
func writeConf(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "test.conf")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const fullConf = `
[Interface]
PrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=
Address = 10.200.200.2/32
DNS = 10.200.200.1, 1.1.1.1
MTU = 1400

[Peer]
PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
PresharedKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25

[Socks5]
BindAddress = 127.0.0.1:1080
`

func TestParseFull(t *testing.T) {
	got, err := Parse(writeConf(t, fullConf))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.PrivateKey != "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=" {
		t.Errorf("PrivateKey = %q", got.PrivateKey)
	}
	if got.Address != netip.MustParseAddr("10.200.200.2") {
		t.Errorf("Address = %v", got.Address)
	}
	wantDNS := []netip.Addr{netip.MustParseAddr("10.200.200.1"), netip.MustParseAddr("1.1.1.1")}
	if !slices.Equal(got.DNS, wantDNS) {
		t.Errorf("DNS = %v, want %v", got.DNS, wantDNS)
	}
	if got.MTU != 1400 {
		t.Errorf("MTU = %d, want 1400", got.MTU)
	}
	if got.Peer.PublicKey != "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" {
		t.Errorf("PublicKey = %q", got.Peer.PublicKey)
	}
	if got.Peer.PresharedKey == "" {
		t.Error("PresharedKey lost")
	}
	if got.Peer.Endpoint != "vpn.example.com:51820" {
		t.Errorf("Endpoint = %q", got.Peer.Endpoint)
	}
	wantIPs := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	if !slices.Equal(got.Peer.AllowedIPs, wantIPs) {
		t.Errorf("AllowedIPs = %v, want %v", got.Peer.AllowedIPs, wantIPs)
	}
	if got.Peer.Keepalive != 25 {
		t.Errorf("Keepalive = %d, want 25", got.Peer.Keepalive)
	}
	if got.Bind != "127.0.0.1:1080" {
		t.Errorf("Bind = %q", got.Bind)
	}
}

const minimalConf = `
[Interface]
PrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=
Address = 10.200.200.2

[Peer]
PublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
Endpoint = 192.0.2.10:51820
AllowedIPs = 10.0.0.5
`

func TestParseDefaults(t *testing.T) {
	got, err := Parse(writeConf(t, minimalConf))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	if got.MTU != 1420 {
		t.Errorf("MTU = %d, want default 1420", got.MTU)
	}
	if got.Bind != "127.0.0.1:25344" {
		t.Errorf("Bind = %q, want default 127.0.0.1:25344", got.Bind)
	}
	if len(got.DNS) != 0 {
		t.Errorf("DNS = %v, want empty", got.DNS)
	}
	if got.Peer.Keepalive != 0 {
		t.Errorf("Keepalive = %d, want 0", got.Peer.Keepalive)
	}
	if got.Peer.PresharedKey != "" {
		t.Errorf("PresharedKey = %q, want empty", got.Peer.PresharedKey)
	}
	// A bare AllowedIPs address means the single-host block.
	want := netip.MustParsePrefix("10.0.0.5/32")
	if !slices.Equal(got.Peer.AllowedIPs, []netip.Prefix{want}) {
		t.Errorf("AllowedIPs = %v, want [%v]", got.Peer.AllowedIPs, want)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name    string
		conf    string
		wantErr string
	}{
		{
			name:    "missing interface section",
			conf:    "[Peer]\nPublicKey = x\n",
			wantErr: "missing [Interface]",
		},
		{
			name:    "missing peer section",
			conf:    "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = 10.0.0.2\n",
			wantErr: "missing [Peer]",
		},
		{
			name:    "missing private key",
			conf:    "[Interface]\nAddress = 10.0.0.2\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = 192.0.2.1:51820\nAllowedIPs = 0.0.0.0/0\n",
			wantErr: "missing PrivateKey",
		},
		{
			name:    "bad private key",
			conf:    "[Interface]\nPrivateKey = short\nAddress = 10.0.0.2\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = 192.0.2.1:51820\nAllowedIPs = 0.0.0.0/0\n",
			wantErr: "PrivateKey",
		},
		{
			name:    "bad address",
			conf:    "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = banana\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = 192.0.2.1:51820\nAllowedIPs = 0.0.0.0/0\n",
			wantErr: "Address",
		},
		{
			name:    "endpoint without port",
			conf:    "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = 10.0.0.2\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = vpn.example.com\nAllowedIPs = 0.0.0.0/0\n",
			wantErr: "Endpoint",
		},
		{
			name:    "missing allowed ips",
			conf:    "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = 10.0.0.2\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = 192.0.2.1:51820\n",
			wantErr: "AllowedIPs",
		},
		{
			name:    "bad mtu",
			conf:    "[Interface]\nPrivateKey = AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=\nAddress = 10.0.0.2\nMTU = abc\n[Peer]\nPublicKey = AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=\nEndpoint = 192.0.2.1:51820\nAllowedIPs = 0.0.0.0/0\n",
			wantErr: "MTU",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(writeConf(t, tt.conf))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseMissingFile(t *testing.T) {
	if _, err := Parse(filepath.Join(t.TempDir(), "nope.conf")); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}
