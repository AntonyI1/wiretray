package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// UAPI renders the device configuration in WireGuard's UAPI wire format:
// one key=value per line, keys in hex. The endpoint arrives already
// resolved because the UAPI accepts only IP:port, never hostnames.
// Line order matters: device keys first, then public_key opens the peer
// and every later line applies to that peer.
func (c *Config) UAPI(endpoint netip.AddrPort) (string, error) {
	priv, err := keyToHex(c.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("PrivateKey: %w", err)
	}
	pub, err := keyToHex(c.Peer.PublicKey)
	if err != nil {
		return "", fmt.Errorf("PublicKey: %w", err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", priv)
	fmt.Fprintf(&b, "public_key=%s\n", pub)
	if c.Peer.PresharedKey != "" {
		psk, err := keyToHex(c.Peer.PresharedKey)
		if err != nil {
			return "", fmt.Errorf("PresharedKey: %w", err)
		}
		fmt.Fprintf(&b, "preshared_key=%s\n", psk)
	}
	fmt.Fprintf(&b, "endpoint=%s\n", endpoint)
	for _, p := range c.Peer.AllowedIPs {
		fmt.Fprintf(&b, "allowed_ip=%s\n", p)
	}
	if c.Peer.Keepalive > 0 {
		fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", c.Peer.Keepalive)
	}
	return b.String(), nil
}

// EndpointUAPI renders the minimal fragment that moves the existing peer
// to a new endpoint without touching anything else. update_only guards
// against accidentally creating a second peer.
func (c *Config) EndpointUAPI(endpoint netip.AddrPort) (string, error) {
	pub, err := keyToHex(c.Peer.PublicKey)
	if err != nil {
		return "", fmt.Errorf("PublicKey: %w", err)
	}
	return fmt.Sprintf("public_key=%s\nupdate_only=true\nendpoint=%s\n", pub, endpoint), nil
}
