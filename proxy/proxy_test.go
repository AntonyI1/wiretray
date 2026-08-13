package proxy

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestBenignTeardown(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"writeto tcp 127.0.0.1:25344->127.0.0.1:50592: read tcp: wsarecv: An established connection was aborted by the software in your host machine.", true},
		{"read tcp: connection reset by peer", true},
		{"write tcp: broken pipe", true},
		{"accept tcp: use of closed network connection", true},
		{"wsarecv: An existing connection was forcibly closed by the remote host.", true},
		{"server: EOF", true},
		{"resolve intranet.example: no such host", false},
		{"bind 127.0.0.1:25344: address already in use", false},
	}
	for _, tt := range tests {
		if got := benignTeardown(tt.msg); got != tt.want {
			t.Errorf("benignTeardown(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestS5LogDemotesBenignErrors(t *testing.T) {
	var buf bytes.Buffer
	l := s5log{slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}

	l.Errorf("read tcp: %s", "connection reset by peer")
	if !strings.Contains(buf.String(), "level=DEBUG") {
		t.Errorf("benign teardown logged as %q, want DEBUG", buf.String())
	}

	buf.Reset()
	l.Errorf("resolve %s: no such host", "gone.example")
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Errorf("real failure logged as %q, want ERROR", buf.String())
	}
}
