package config

import "net/netip"

// Config is one tunnel definition, read from a wg-quick style .conf file
// with an added [Socks5] section. Fields mirror the file keys.
type Config struct {
	PrivateKey string       // base64, converted to hex only when building UAPI
	Address    netip.Addr   // the tunnel's inner address
	DNS        []netip.Addr // resolvers used inside the tunnel
	MTU        int          // 1420 unless the file says otherwise
	Peer       Peer
	Bind       string // SOCKS listen address, 127.0.0.1:25344 unless set
}

// Peer is the single [Peer] section: the server side of the tunnel.
type Peer struct {
	PublicKey    string // base64
	PresharedKey string // base64, empty when unused
	Endpoint     string // host:port as written; resolved at start time
	AllowedIPs   []netip.Prefix
	Keepalive    int // seconds, 0 disables keepalive
}
