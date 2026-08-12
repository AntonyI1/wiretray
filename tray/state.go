package tray

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type persisted struct {
	Config string `json:"config"`
}

func statePath(dir string) string { return filepath.Join(dir, "state.json") }

// loadSelected returns the previously chosen config path, or empty.
func loadSelected(dir string) string {
	raw, err := os.ReadFile(statePath(dir))
	if err != nil {
		return ""
	}
	var p persisted
	if json.Unmarshal(raw, &p) != nil {
		return ""
	}
	return p.Config
}

func saveSelected(dir, conf string) {
	raw, _ := json.Marshal(persisted{Config: conf})
	_ = os.WriteFile(statePath(dir), raw, 0o600)
}
