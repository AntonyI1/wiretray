# WireTray

A WireGuard client that lives in the system tray and never touches your routing table.

I run Tailscale for my home network and a company WireGuard VPN for work. Both want to own the machine's routes, so the official clients cannot run at the same time. WireTray fixes this by running the work tunnel entirely in userspace: wireguard-go speaks the protocol, gVisor's network stack stands in for a kernel interface, and the result is exposed as a SOCKS5 proxy on 127.0.0.1. A dedicated Firefox profile points at that proxy and gets the work network. Nothing else on the machine knows the tunnel exists.

No network interface. No routes. No admin rights. Nothing for the two VPNs to fight over.

Status: design phase. The design is in [docs/design.md](docs/design.md).
