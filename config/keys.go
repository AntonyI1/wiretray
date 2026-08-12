package config

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// keyToHex converts a WireGuard key from the base64 form used in config
// files to the hex form the device UAPI expects.
func keyToHex(b64 string) (string, error) {
	decoded, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("decode key: %w", err)
	}
	if len(decoded) != 32 {
		return "", fmt.Errorf("key is %d bytes after decoding, want 32", len(decoded))
	}
	return hex.EncodeToString(decoded), nil
}
