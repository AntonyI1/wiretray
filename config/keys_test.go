package config

import "testing"

// WireGuard keys are 32 bytes. Config files store them base64 encoded,
// the device UAPI wants them hex encoded.
func TestKeyToHex(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "valid key",
			in:   "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyA=",
			want: "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		},
		{
			name: "valid all zero key",
			in:   "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
			want: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:    "not base64",
			in:      "this is not a key!!",
			wantErr: true,
		},
		{
			name:    "wrong length",
			in:      "AQIDBAUGBwgJCgsMDQ4PEA==",
			wantErr: true,
		},
		{
			name:    "empty",
			in:      "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := keyToHex(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("keyToHex(%q) expected an error, got %q", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("keyToHex(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("keyToHex(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
