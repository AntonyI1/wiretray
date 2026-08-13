# WireTray design

## The problem

Tailscale and a work WireGuard tunnel cannot run side by side with the official clients. `wg-quick` with `AllowedIPs = 0.0.0.0/0` claims the default route (via fwmark and policy routing), and Tailscale also manages system routes. Two owners of the default route is a routing-table collision, not a WireGuard bug.

What I actually want is per-application tunneling: one browser profile on the work VPN, everything else untouched. No desktop WireGuard client offers that (only the Android client does).

## The approach

Layering instead of arbitration. Tailscale stays the only owner of system networking. The work tunnel runs as a userspace process: wireguard-go speaks the protocol, gVisor's netstack stands in for a kernel interface, and the whole thing is exposed as a SOCKS5 proxy on 127.0.0.1. Only applications pointed at the proxy use the tunnel. No interface, no routes, no admin rights. The two VPNs cannot fight because they never touch the same layer.

Alternatives considered:

- **Narrowing AllowedIPs.** Operates on destination IPs, not applications. Cannot express "this browser profile yes, everything else no."
- **A browser extension.** Not possible. WebExtensions have no access to UDP sockets or network interfaces, so no extension can implement a VPN protocol. Store "VPN extensions" are proxy clients or control panels for native apps.
- **Network namespaces.** Real per-app isolation, but Linux only. The target here is Windows.
- **Driver-level per-app tunneling (WireSock, TunnlTo).** Works, but it filters at the Windows driver level, needs admin, and the engine is closed source, which means your VPN credentials go into code you cannot read.
- **A daemon plus a tray client over IPC.** Two processes, two lifecycles, a service install. Robustness a personal tool does not need; a single process is simpler to reason about and to test.

wireproxy already proves the userspace approach and WireTray keeps its config format. What wireproxy lacks is a UI: a tray toggle, visible handshake state, and a config picker. That is the gap this project fills.

## Decisions

- Windows first. The core packages are OS-agnostic and the UI layer is isolated behind build tags, so other platforms can follow.
- Single binary, tray-owned. The tray app hosts the device, the netstack, and the SOCKS listener in one process.
- SOCKS5 on loopback only by default. Widening the bind is always an explicit config act.
- Plaintext wg-quick-compatible configs under the user config dir. No keychain integration.
- One active tunnel at a time, chosen from a picker. Simultaneous tunnels are out of scope.
- Fail-closed by design: with the app in its default posture, the SOCKS listener exists only while the tunnel is up. App not running or tunnel down means connection refused, never a silent fallback onto normal routing.
- Direct fallback is the one deliberate exception: an off-by-default tray toggle that keeps the port open when the tunnel is down and routes over the normal network. It exists for always-on proxy setups (a permanently exported ALL_PROXY, a dedicated profile browsing public sites offline from the VPN). While active the tray shows a blue state that cannot be mistaken for connected, because the entire risk of the mode is forgetting it is on.

## Architecture

Five units, one job each:

| Package | Responsibility | Depends on |
|---|---|---|
| `config` | Parse wg-quick-format `.conf` plus `[Socks5]` section into a typed Config; base64 to hex key conversion; UAPI string building; validation | stdlib, ini |
| `engine` | Own wireguard-go device and netstack lifecycle: `Start(cfg)`, `Stop()`, `Status()`; expose the netstack `tnet` for dialing | wireguard-go, netstack |
| `proxy` | SOCKS5 server; dialer and resolver come from a swappable backend (the engine's netstack, or the OS directly in fallback mode) | go-socks5 |
| `tray` | systray UI: states, toggle, config picker, last-handshake display; autostart registration (build-tagged per OS) | fyne systray |
| `cmd/wiretray` | Wiring, logging, `--no-tray` headless mode | all above |

```
.conf file ──> config ──> engine (IpcSet, hex UAPI) ──> wireguard-go device
                                      │                        │
                                      └── netstack tnet <──────┘
                                             ▲  ▲
                              DialContext ───┘  └─── LookupContextHost
                                      │              │
Browser (socks5h) ──> proxy [127.0.0.1:25344] ───────┘
```

`--no-tray` exists because CI has no desktop, and because a headless proxy is useful on its own.

## Data flow

1. The user picks a config. `config` parses it, resolves the peer `Endpoint` hostname with the system resolver on purpose (the outer tunnel packet must travel over normal networking), converts keys from base64 to hex, and builds the UAPI string.
2. `engine` creates the netstack TUN (`netstack.CreateNetTUN` with Address, DNS, MTU), applies the UAPI config via `IpcSet`, brings the device up, and polls `IpcGet` until the first handshake.
3. `proxy` starts a SOCKS5 listener whose dialer is `tnet.DialContext` and whose resolver wraps `tnet.LookupContextHost`.
4. The browser (with "Proxy DNS when using SOCKS v5" ticked) sends hostnames to the proxy, and the resolver path answers them at the `[Interface] DNS` server inside the tunnel.

The resolver injection is mandatory, not optional. go-socks5's default resolver uses the OS resolver, which would send internal hostnames to the local DNS instead. That fails or leaks exactly what the proxied-DNS setting exists to prevent.

## States and error handling

`disconnected -> connecting -> connected -> error`, plus `fallback` (blue): the port is open but traffic goes direct because the user enabled the fallback toggle and the tunnel is down.

- **connecting:** after `IpcSet` and `Up`, poll `IpcGet` for `last_handshake_time_sec` every 500ms. The first nonzero value means connected. 15 seconds without one means error ("no handshake: check keys, endpoint, UDP reachability"). A wrong key encoding otherwise fails silently; this timeout makes it visible.
- **connected:** poll every 10s. Handshake age over 180s shows a stale note in the tooltip. The state stays connected because WireGuard rekeys lazily.
- **stop:** close the listener, then `dev.Down()`, then `dev.Close()`, then netstack teardown. Toggling must be idempotent and leak nothing, which the tests assert.
- **start failures** (unparseable conf, endpoint DNS failure, port already bound) produce an error state with the reason in the tooltip and the log. No partial states: a failed start tears down whatever came up.
- **Logging:** `slog`, text handler, stderr plus a single file under the config dir (truncate at 5MB, no rotation machinery).
- **Later:** endpoint re-resolution on stall (re-apply the endpoint after sustained handshake failure), which covers VPN server IP changes and sleep/wake.

## On-disk layout

```
os.UserConfigDir()/wiretray/        (Windows: %AppData%\wiretray)
  configs/*.conf     wg-quick format plus [Socks5], wireproxy-compatible
  state.json         last selected config, autostart flag
  wiretray.log
```

Config format is wireproxy's: `[Interface]` PrivateKey, Address, DNS, optional MTU (default 1420); `[Peer]` PublicKey, optional PresharedKey, Endpoint, AllowedIPs, PersistentKeepalive; `[Socks5]` BindAddress.

Parsing rules: keys are base64 in files and hex in UAPI; Address and DNS accept a bare IP or CIDR (parse the prefix, keep the address); `allowed_ip` is one UAPI line per entry.

First run with no configs: the tray shows disconnected with a "no configs found, open config folder" menu item in place of the toggle. Headless mode exits with the same message.

## Testing

- **Unit (`config`):** base64 to hex, CIDR stripping, golden UAPI strings, validation failures.
- **Integration:** two in-process WireGuard peers. Peer A is a second netstack device with a known keypair and an HTTP server inside its netstack. Peer B is the real engine plus proxy stack. The handshake is real, over loopback UDP, and the test drives an HTTP GET through the actual SOCKS listener. No root, no network, no test infrastructure.
- **Resolver path:** unit-tested with an injected fake, plus an assertion that hostname requests reach the resolver rather than the OS.
- **Manual acceptance:** a real tunnel and a dedicated browser profile: internal sites load while Tailscale is up, `curl --socks5-hostname` works.
- **CI:** ubuntu runs vet and tests; a windows amd64 binary is cross-built in the same workflow.

## Phases

| Phase | Scope | Done when |
|---|---|---|
| 1: core | `config`, `engine`, `proxy`, `cmd --no-tray` | Integration test green in CI; a browser profile and `curl --socks5-hostname` reach tunnel-internal resources while Tailscale is up |
| 2: lifecycle | Idempotent start/stop, status API, endpoint re-resolve on stall | Repeated toggling shows no goroutine or port leak |
| 3: tray | Four states with distinct icons, toggle, config picker, last-handshake line, autostart | Daily use replaces manual VPN toggling |
| 4: on demand | Extra bind addresses (for WSL), HTTP CONNECT listener, static TCP forwards, PAC file | Each lands only when actually wanted |

## Non-goals

Kernel interface mode (the whole point is not touching routing). ICMP and raw sockets (not proxyable; use namespaces if needed). UDP ASSOCIATE, for now (a browser over SOCKS needs TCP plus proxied DNS only). Simultaneous multi-tunnel. Server-side WireGuard. Keychain integration. An installer.

## Menu behavior

Field testing exposed a Windows reality: the tray library's menu does
not close when an item is clicked, which is nonstandard, and a menu left
open can then be dismissed by unrelated icon repaints at random moments.
WireTray restores standard semantics itself: every item click first ends
menu mode synchronously (targeting its own tray window by process id,
since all apps built on the same library share a window class), and only
then applies the action and any repaint. Click, close, act, recolor,
deterministically.

## Operational caveats

- Split tunneling means a workplace VPN sees only browser traffic. Check that this is acceptable with whoever issued the config.
- If Tailscale uses an exit node, the tunnel's outer UDP packets travel through it, so the VPN server sees the exit node's address. Harmless technically, but it can trip source-IP alerting.
- Tools that do not speak SOCKS (raw `ping`, anything on raw sockets) will not work through this. That is a protocol limit, not a bug.

## Implementation notes

1. `IpcSet` takes hex keys and config files carry base64. Wrong encoding gives a silent no-handshake; see the connecting timeout above.
2. go-socks5's default resolver is the OS resolver. Inject the netstack resolver or internal DNS leaks.
3. `netip.MustParseAddr` rejects CIDR (`10.0.0.2/32`). Parse as a prefix and take the address.
4. UAPI `endpoint` requires `IP:port`. Resolve the hostname with the system resolver before `IpcSet`, deliberately outside the tunnel.
5. wireguard-go and gvisor are pseudo-versioned and must be a matched pair in `go.mod`. Start from wireproxy's known-good pins.
6. Keep `tray` build-tagged so Linux CI builds and tests everything else.

## Prior art

- **wireproxy**: the reference for the userspace approach. Headless, no UI. WireTray stays config-compatible with it.
- **WireSock / TunnlTo**: per-app WireGuard on Windows at the driver level. Closed engine, admin required.
- **wgtray**: a Linux tray toggle for wg-quick. Lives in the kernel and routing world this project deliberately avoids.
- **Official WireGuard clients**: per-app tunneling exists only on Android.
