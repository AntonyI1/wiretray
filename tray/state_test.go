package tray

import "testing"

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()

	if got := loadState(dir); got != (persisted{}) {
		t.Errorf("fresh dir: loadState = %+v, want zero", got)
	}

	want := persisted{Config: `C:\somewhere\work.conf`, Fallback: true}
	saveState(dir, want)
	if got := loadState(dir); got != want {
		t.Errorf("loadState = %+v after save, want %+v", got, want)
	}
}
