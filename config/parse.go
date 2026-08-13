package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"gopkg.in/ini.v1"
)

const (
	defaultMTU = 1420

	// DefaultBind is where the SOCKS listener sits when a config does
	// not say otherwise; exported because the tray needs it when no
	// config is selected yet.
	DefaultBind = "127.0.0.1:25344"
)

// Parse reads a wg-quick style .conf with an added [Socks5] section.
// It validates everything the engine later relies on, so a Config that
// parses is a Config that can at least attempt a handshake.
func Parse(path string) (*Config, error) {
	f, err := ini.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	iface, err := f.GetSection("Interface")
	if err != nil {
		return nil, fmt.Errorf("%s: missing [Interface] section", path)
	}
	peerSec, err := f.GetSection("Peer")
	if err != nil {
		return nil, fmt.Errorf("%s: missing [Peer] section", path)
	}

	cfg := &Config{MTU: defaultMTU, Bind: DefaultBind}

	if cfg.PrivateKey, err = requiredKey(iface, "PrivateKey"); err != nil {
		return nil, err
	}
	if _, err := keyToHex(cfg.PrivateKey); err != nil {
		return nil, fmt.Errorf("PrivateKey: %w", err)
	}

	addr, err := requiredKey(iface, "Address")
	if err != nil {
		return nil, err
	}
	if cfg.Address, err = parseAddr(addr); err != nil {
		return nil, fmt.Errorf("Address: %w", err)
	}

	for _, s := range iface.Key("DNS").Strings(",") {
		a, err := parseAddr(s)
		if err != nil {
			return nil, fmt.Errorf("DNS: %w", err)
		}
		cfg.DNS = append(cfg.DNS, a)
	}

	if v := iface.Key("MTU").String(); v != "" {
		if cfg.MTU, err = iface.Key("MTU").Int(); err != nil {
			return nil, fmt.Errorf("MTU: %w", err)
		}
	}

	if cfg.Peer, err = parsePeer(peerSec); err != nil {
		return nil, err
	}

	if v := f.Section("Socks5").Key("BindAddress").String(); v != "" {
		if _, _, err := net.SplitHostPort(v); err != nil {
			return nil, fmt.Errorf("BindAddress: %w", err)
		}
		cfg.Bind = v
	}

	return cfg, nil
}

func parsePeer(sec *ini.Section) (Peer, error) {
	var p Peer
	var err error

	if p.PublicKey, err = requiredKey(sec, "PublicKey"); err != nil {
		return Peer{}, err
	}
	if _, err := keyToHex(p.PublicKey); err != nil {
		return Peer{}, fmt.Errorf("PublicKey: %w", err)
	}

	if p.PresharedKey = sec.Key("PresharedKey").String(); p.PresharedKey != "" {
		if _, err := keyToHex(p.PresharedKey); err != nil {
			return Peer{}, fmt.Errorf("PresharedKey: %w", err)
		}
	}

	if p.Endpoint, err = requiredKey(sec, "Endpoint"); err != nil {
		return Peer{}, err
	}
	if _, _, err := net.SplitHostPort(p.Endpoint); err != nil {
		return Peer{}, fmt.Errorf("Endpoint: %w", err)
	}

	ips := sec.Key("AllowedIPs").Strings(",")
	if len(ips) == 0 {
		return Peer{}, fmt.Errorf("missing AllowedIPs in [Peer]")
	}
	for _, s := range ips {
		pfx, err := parsePrefix(s)
		if err != nil {
			return Peer{}, fmt.Errorf("AllowedIPs: %w", err)
		}
		p.AllowedIPs = append(p.AllowedIPs, pfx)
	}

	if v := sec.Key("PersistentKeepalive").String(); v != "" {
		if p.Keepalive, err = sec.Key("PersistentKeepalive").Int(); err != nil {
			return Peer{}, fmt.Errorf("PersistentKeepalive: %w", err)
		}
	}

	return p, nil
}

// parsePrefix reads a CIDR block, accepting a bare address as the
// single-host block wg-quick treats it as ("10.0.0.5" means /32).
func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("parse prefix %q: %w", s, err)
		}
		return p, nil
	}
	a, err := parseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func requiredKey(sec *ini.Section, name string) (string, error) {
	v := strings.TrimSpace(sec.Key(name).String())
	if v == "" {
		return "", fmt.Errorf("missing %s in [%s]", name, sec.Name())
	}
	return v, nil
}
