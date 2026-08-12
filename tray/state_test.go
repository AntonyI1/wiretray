package tray

import "testing"

func TestSelectedRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if got := loadSelected(dir); got != "" {
		t.Errorf("fresh dir: loadSelected = %q, want empty", got)
	}

	saveSelected(dir, `C:\somewhere\work.conf`)
	if got := loadSelected(dir); got != `C:\somewhere\work.conf` {
		t.Errorf("loadSelected = %q after save", got)
	}
}
