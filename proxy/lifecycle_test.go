package proxy

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

type nilLookup struct{}

func (nilLookup) LookupContextHost(context.Context, string) ([]string, error) {
	return nil, nil
}

// TestRepeatedListenClose proves the port is actually released on Close:
// rebinding the same address ten times only works if each cycle lets go.
func TestRepeatedListenClose(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	s := &Server{log: log}
	if err := s.Listen("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	addr := s.Addr().String()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		s := &Server{log: log}
		if err := s.Listen(addr); err != nil {
			t.Fatalf("cycle %d: %v", i, err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("cycle %d close: %v", i, err)
		}
	}
}
