# <img src="docs/logo.png" alt="" width="36"> WireTray

A WireGuard client that lives in the system tray and never touches your routing table.

I run Tailscale for my home network and a company WireGuard VPN for work. Both want to own the machine's routes, so the official clients cannot run at the same time. WireTray fixes this by running the work tunnel entirely in userspace: wireguard-go speaks the protocol, gVisor's network stack stands in for a kernel interface, and the result is exposed as a SOCKS5 proxy on 127.0.0.1. Anything pointed at that proxy uses the tunnel. Nothing else on the machine knows it exists.

No network interface. No routes. No admin rights. Nothing for two VPNs to fight over.

The design and the reasoning behind it are in [docs/design.md](docs/design.md).

## Setup

Build it (Go 1.26 or later):

```
go build ./cmd/wiretray
```

Cross-compiling the Windows tray build from Linux or WSL:

```
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui" -o wiretray.exe ./cmd/wiretray
```

Or grab the exe from the releases page.

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

Start `wiretray.exe`. A gray constellation appears in the tray. The menu has Connect, a tunnel picker (when you have more than one config), Open config folder, Allow direct fallback, Start at login, and Quit.

Click Connect: the dot turns amber while the handshake runs, green when the tunnel is live, red with the reason in the menu if something is wrong. Handshake age shows in the tooltip. If the VPN server stops answering, WireTray re-resolves its address and recovers on its own.

When the tunnel is off, the proxy port is closed. Anything pointed at it gets connection refused rather than silently using your normal network. If you would rather have things keep working, the "Allow direct fallback" toggle (off unless you turn it on) keeps the port open when the tunnel is down and sends traffic over your normal network instead. The dots turn blue while that is happening, as an unmissable reminder that nothing is tunneled: internal names will not resolve, and your traffic takes its ordinary path.

`wiretray -no-tray` runs the same core headless in a terminal. Logs go to stderr and to `wiretray.log` next to the config folder.

## Point apps at it

Any app that can speak SOCKS5 can use the tunnel, and only the apps you point at it are affected. When an app offers a "proxy DNS" or "remote DNS" option, turn it on so name lookups happen inside the tunnel too.

**Firefox.** Use a dedicated profile (`about:profiles` to make one). In that profile: Settings, Network Settings, Manual proxy configuration, SOCKS Host `127.0.0.1`, Port `25344`, SOCKS v5, and tick "Proxy DNS when using SOCKS v5". Both profiles can run at once, so one Firefox window is on the work network and another is not.

**Chrome and Edge.** There is no per-profile proxy setting, but a dedicated shortcut does the same job:

```
chrome --profile-directory="Work" --proxy-server="socks5://127.0.0.1:25344" --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE 127.0.0.1"
```

The resolver rule stops Chrome from doing DNS locally, which is its version of the Firefox tick.

**Terminals.**

```
curl --socks5-hostname 127.0.0.1:25344 https://internal.example
export ALL_PROXY=socks5h://127.0.0.1:25344
```

The `h` in `socks5h` sends DNS through the tunnel. Most command line tools respect `ALL_PROXY`.

**SSH to an internal host**, on systems whose `nc` supports proxying:

```
ssh -o ProxyCommand="nc -X 5 -x 127.0.0.1:25344 %h %p" user@internal-host
```

## From WSL

WSL2's default NAT networking cannot reach a Windows loopback listener. The clean fix is mirrored networking, which makes localhost genuinely shared between Windows and WSL. In `%UserProfile%\.wslconfig`:

```ini
[wsl2]
networkingMode=mirrored
```

Then run `wsl --shutdown` from PowerShell and reopen your terminal. After that the same `ALL_PROXY=socks5h://127.0.0.1:25344` recipe works inside WSL, with WireTray still running on the Windows side. Mirrored mode also lets WSL reach Tailscale addresses, which NAT mode never did.

With direct fallback enabled you can even leave `ALL_PROXY` exported permanently: work traffic tunnels when connected, and everything degrades to your normal network instead of breaking when the tunnel is off.

## Measured behavior

All numbers come from the in-repo harness, which runs both tunnel endpoints inside one process over loopback: every byte crosses two complete userspace stacks, and no physical network is involved. Reproduce with:

```
go test ./engine -bench . -benchtime=5s -count=3 -run '^$'
WIRETRAY_SOAK=1 go test ./engine -run TestSoak -v -timeout 20m
```

On an i9-14900HX under WSL2:

- Throughput settles around 80 to 90 MB/s once warm, through SOCKS5, gVisor TCP, and WireGuard encryption on both sides. One side alone does roughly double, so a real VPN link is the bottleneck long before this stack is.
- Connecting takes about 49ms from start to completed handshake, stable across hundreds of cycles.
- A ten minute soak under continuous requests: 120 of 120 succeeded, the handshake never aged past a normal rekey interval, and the goroutine count stayed flat.
- The deployed app idles under 40MB while connected. The binary is a single static 12MB exe with no runtime dependencies.

## What it will not do

ICMP and raw sockets cannot cross a SOCKS proxy, so `ping` will not work through it. It runs one tunnel at a time. It is a client only. See the non-goals section of the design doc.
