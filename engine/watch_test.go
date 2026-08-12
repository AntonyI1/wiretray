package engine

import (
	"testing"
	"time"
)

func TestStalled(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		last time.Time
		want bool
	}{
		{name: "never handshook", last: time.Time{}, want: false},
		{name: "fresh", last: now.Add(-30 * time.Second), want: false},
		{name: "at the boundary", last: now.Add(-staleAfter), want: false},
		{name: "stalled", last: now.Add(-staleAfter - time.Second), want: true},
	}
	for _, tt := range tests {
		if got := stalled(now, tt.last); got != tt.want {
			t.Errorf("%s: stalled = %v, want %v", tt.name, got, tt.want)
		}
	}
}
