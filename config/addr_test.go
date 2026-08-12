package config

import "testing"

func TestParseAddr(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "bare ipv4", in: "10.200.200.2", want: "10.200.200.2"},
		{name: "ipv4 with cidr", in: "10.200.200.2/32", want: "10.200.200.2"},
		{name: "cidr keeps the address not the network", in: "10.200.200.2/24", want: "10.200.200.2"},
		{name: "bare ipv6", in: "2001:db8::2", want: "2001:db8::2"},
		{name: "ipv6 with cidr", in: "2001:db8::2/64", want: "2001:db8::2"},
		{name: "garbage", in: "banana", wantErr: true},
		{name: "bad suffix", in: "10.0.0.2/banana", wantErr: true},
		{name: "empty", in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAddr(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseAddr(%q) expected an error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseAddr(%q) unexpected error: %v", tt.in, err)
			}
			if got.String() != tt.want {
				t.Errorf("parseAddr(%q) = %v, want %s", tt.in, got, tt.want)
			}
		})
	}
}
