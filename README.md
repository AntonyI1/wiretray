# WireTray

A WireGuard client that lives in the system tray and never touches your routing table.

I run Tailscale for my home network and a company WireGuard VPN for work. Both want to own the machine's routes, so the official clients cannot run at the same time. WireTray fixes this by running the work tunnel entirely in userspace: wireguard-go speaks the protocol, gVisor's network stack stands in for a kernel interface, and the result is exposed as a SOCKS5 proxy on 127.0.0.1. A dedicated Firefox profile points at that proxy and gets the work network. Nothing else on the machine knows the tunnel exists.

No network interface. No routes. No admin rights. Nothing for the two VPNs to fight over.

The design and the reasoning behind it are in [docs/design.md](docs/design.md).

## Setup

Build it (Go 1.26 or later):

```
go build ./cmd/wiretray
```

Cross-compiling the Windows tray build from Linux or WSL:

```
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o wiretray.exe ./cmd/wiretray
```

Put your tunnel config in the config folder (`%AppData%\wiretray\configs\` on Windows). The format is the same as wg-quick, plus an optional `[Socks5]` section:

```ini
[Interface]
PrivateKey = <your key, base64>
Address = 10.200.200.2/32
DNS = 10.200.200.1

[Peer]
PublicKey = <server key, base64>
Endpoint = vpn.example.com:51820
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25

[Socks5]
BindAddress = 127.0.0.1:25344
```

## Running it

Start `wiretray.exe`. A gray dot appears in the tray. The menu has Connect, a tunnel picker (when you have more than one config), Open config folder, Start at login, and Quit.

Click Connect: the dot turns amber while the handshake runs, green when the tunnel is live, red with the reason in the menu if something is wrong. Handshake age shows in the tooltip. If the VPN server stops answering, WireTray re-resolves its address and recovers on its own.

Point a browser at it. In Firefox, use a dedicated profile: Settings, Network Settings, Manual proxy configuration, SOCKS Host `127.0.0.1`, Port `25344`, SOCKS v5, and tick "Proxy DNS when using SOCKS v5" so name lookups also happen inside the tunnel. Only that profile uses the work network; everything else on the machine is untouched.

When the tunnel is off, the proxy port is closed. Anything pointed at it gets connection refused rather than silently using your normal network.

Terminal use works too:

```
curl --socks5-hostname 127.0.0.1:25344 https://internal.example
```

`wiretray -no-tray` runs the same core headless in a terminal. Logs go to stderr and to `wiretray.log` next to the config folder.

## What it will not do

ICMP and raw sockets cannot cross a SOCKS proxy, so `ping` will not work through it. It runs one tunnel at a time. It is a client only. See the non-goals section of the design doc.
