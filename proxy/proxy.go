package proxy

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/things-go/go-socks5"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

// Server is the SOCKS5 front door. Every connection it accepts is dialed
// inside the tunnel's network stack, never through the OS.
type Server struct {
	s5  *socks5.Server
	ln  net.Listener
	log *slog.Logger
}

func New(tnet *netstack.Net, log *slog.Logger) *Server {
	s5 := socks5.NewServer(
		socks5.WithDial(tnet.DialContext),
		socks5.WithResolver(resolver{tnet}),
		socks5.WithLogger(s5log{log}),
	)
	return &Server{s5: s5, log: log}
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

// Close stops the listener. In-flight streams die with the tunnel, which
// is torn down right after; fail-closed is the point.
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
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
