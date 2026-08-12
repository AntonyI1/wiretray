package proxy

import (
	"context"
	"errors"
	"testing"
)

type fakeLookup struct {
	asked string
	addrs []string
	err   error
}

func (f *fakeLookup) LookupContextHost(_ context.Context, host string) ([]string, error) {
	f.asked = host
	return f.addrs, f.err
}

func TestResolverUsesTunnelLookup(t *testing.T) {
	f := &fakeLookup{addrs: []string{"10.1.2.3"}}

	_, ip, err := resolver{f}.Resolve(context.Background(), "intranet.example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if f.asked != "intranet.example" {
		t.Errorf("tunnel lookup asked for %q, want intranet.example", f.asked)
	}
	if ip.String() != "10.1.2.3" {
		t.Errorf("ip = %v, want 10.1.2.3", ip)
	}
}

func TestResolverPropagatesFailure(t *testing.T) {
	f := &fakeLookup{err: errors.New("no such host")}
	if _, _, err := (resolver{f}).Resolve(context.Background(), "gone.example"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestResolverSkipsUnparseableAddresses(t *testing.T) {
	f := &fakeLookup{addrs: []string{"not an ip", "10.9.9.9"}}

	_, ip, err := resolver{f}.Resolve(context.Background(), "x.example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if ip.String() != "10.9.9.9" {
		t.Errorf("ip = %v, want 10.9.9.9", ip)
	}
}
