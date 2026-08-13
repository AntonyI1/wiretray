package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"

	"github.com/things-go/go-socks5"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// Backend is where the proxy sends traffic: the tunnel's network stack,
// or the operating system directly when fallback mode allows it.
type Backend struct {
	Dial    func(ctx context.Context, network, addr string) (net.Conn, error)
	Resolve hostLookup
}

// NetstackBackend routes everything inside the tunnel.
func NetstackBackend(tnet *netstack.Net) Backend {
	return Backend{Dial: tnet.DialContext, Resolve: tnet}
}

// DirectBackend routes over the normal network: no tunnel involved.
// Only fallback mode uses it, and only by explicit user choice.
func DirectBackend() Backend {
	var d net.Dialer
	return Backend{Dial: d.DialContext, Resolve: directLookup{}}
}

type directLookup struct{}

func (directLookup) LookupContextHost(ctx context.Context, host string) ([]string, error) {
	return net.DefaultResolver.LookupHost(ctx, host)
}

// Server is the SOCKS5 front door. Connections go wherever the active
// backend points; the backend can be swapped while serving.
type Server struct {
	s5      *socks5.Server
	ln      net.Listener
	log     *slog.Logger
	backend atomic.Pointer[Backend]
}

func New(initial Backend, log *slog.Logger) *Server {
	s := &Server{log: log}
	s.backend.Store(&initial)
	s.s5 = socks5.NewServer(
		socks5.WithDial(s.dial),
		socks5.WithResolver(resolver{serverLookup{s}}),
		socks5.WithLogger(s5log{log}),
	)
	return s
}

// SetBackend atomically redirects where new connections go. In-flight
// streams keep the backend they started with.
func (s *Server) SetBackend(b Backend) {
	s.backend.Store(&b)
}

func (s *Server) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return s.backend.Load().Dial(ctx, network, addr)
}

// serverLookup resolves through whatever backend is active right now.
type serverLookup struct{ s *Server }

func (l serverLookup) LookupContextHost(ctx context.Context, host string) ([]string, error) {
	return l.s.backend.Load().Resolve.LookupContextHost(ctx, host)
}

// Listen binds the SOCKS port. Split from Serve so bind errors surface
// before the caller commits, and so tests can bind port 0 and read the
// real address back.
func (s *Server) Listen(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	s.ln = ln
	s.log.Info("socks5 listening", "addr", ln.Addr().String())
	return nil
}

// Addr reports the bound address, useful when Listen was given port 0.
func (s *Server) Addr() net.Addr {
	if s.ln == nil {
		return nil
	}
	return s.ln.Addr()
}

// Serve blocks accepting connections until Close.
func (s *Server) Serve() error {
	if s.ln == nil {
		return errors.New("serve before listen")
	}
	err := s.s5.Serve(s.ln)
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// Close stops the listener.
func (s *Server) Close() error {
	if s.ln == nil {
		return nil
	}
	return s.ln.Close()
}

// s5log adapts slog to the socks5 library's logger interface. Ordinary
// connection churn is demoted to debug so the log only shouts about
// problems that need a human.
type s5log struct{ log *slog.Logger }

func (l s5log) Errorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	if benignTeardown(msg) {
		l.log.Debug(msg)
		return
	}
	l.log.Error(msg)
}

// benignTeardown reports whether a relay error is normal connection
// teardown. Browsers abort SOCKS streams all the time (closed tabs,
// cancelled preloads), and each surfaces as a read or write error.
func benignTeardown(msg string) bool {
	for _, s := range []string{
		"connection was aborted",
		"forcibly closed by the remote host",
		"connection reset by peer",
		"broken pipe",
		"use of closed network connection",
		"EOF",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
