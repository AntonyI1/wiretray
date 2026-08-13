package tray

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type persisted struct {
	Config   string `json:"config"`
	Fallback bool   `json:"fallback"`
}

func statePath(dir string) string { return filepath.Join(dir, "state.json") }

// loadState returns the saved preferences, zero-valued when absent.
func loadState(dir string) persisted {
	raw, err := os.ReadFile(statePath(dir))
	if err != nil {
		return persisted{}
	}
	var p persisted
	if json.Unmarshal(raw, &p) != nil {
		return persisted{}
	}
	return p
}

func saveState(dir string, p persisted) {
	raw, _ := json.Marshal(p)
	_ = os.WriteFile(statePath(dir), raw, 0o600)
}
