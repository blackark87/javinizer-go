package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// withTempUserDataDir redirects the package-level userConfigDirFn to a temp
// dir for the duration of the test so WindowState load/save never touches the
// real user data dir. Returns the resolved UserDataDir.
func withTempUserDataDir(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	orig := userConfigDirFn
	userConfigDirFn = func() (string, error) { return tmp, nil }
	t.Cleanup(func() { userConfigDirFn = orig })
	dir, err := UserDataDir()
	if err != nil {
		t.Fatalf("UserDataDir: %v", err)
	}
	return dir
}

func TestLoadWindowState_MissingFileReturnsZero(t *testing.T) {
	withTempUserDataDir(t)

	got, err := LoadWindowState()
	if err != nil {
		t.Fatalf("LoadWindowState: %v", err)
	}
	if got != (WindowState{}) {
		t.Fatalf("expected zero WindowState, got %+v", got)
	}
}

func TestSaveWindowState_RoundTrip(t *testing.T) {
	withTempUserDataDir(t)

	want := WindowState{Width: 1400, Height: 950, Maximised: false}
	if err := SaveWindowState(want); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	got, err := LoadWindowState()
	if err != nil {
		t.Fatalf("LoadWindowState: %v", err)
	}
	if got != want {
		t.Fatalf("expected %+v, got %+v", want, got)
	}
}

func TestSaveWindowState_PersistsMaximised(t *testing.T) {
	withTempUserDataDir(t)

	want := WindowState{Width: 1280, Height: 800, Maximised: true}
	if err := SaveWindowState(want); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	got, _ := LoadWindowState()
	if !got.Maximised {
		t.Fatalf("expected Maximised=true, got %+v", got)
	}
}

func TestLoadWindowState_CorruptFileReturnsZero(t *testing.T) {
	dir := withTempUserDataDir(t)

	if err := os.WriteFile(filepath.Join(dir, windowStateFile), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	got, err := LoadWindowState()
	if err != nil {
		t.Fatalf("corrupt file should not error, got %v", err)
	}
	if got != (WindowState{}) {
		t.Fatalf("corrupt file should yield zero state, got %+v", got)
	}
}

func TestClampWindowState_BelowMinClampedUp(t *testing.T) {
	got := clampWindowState(WindowState{Width: 100, Height: 100})
	if got.Width != windowStateMinWidth {
		t.Errorf("Width = %d, want min %d", got.Width, windowStateMinWidth)
	}
	if got.Height != windowStateMinHeight {
		t.Errorf("Height = %d, want min %d", got.Height, windowStateMinHeight)
	}
}

func TestClampWindowState_AboveMaxClampedDown(t *testing.T) {
	got := clampWindowState(WindowState{Width: 99999, Height: 99999})
	if got.Width != windowStateMaxWidth {
		t.Errorf("Width = %d, want max %d", got.Width, windowStateMaxWidth)
	}
	if got.Height != windowStateMaxHeight {
		t.Errorf("Height = %d, want max %d", got.Height, windowStateMaxHeight)
	}
}

func TestClampWindowState_ZeroReturnsZero(t *testing.T) {
	got := clampWindowState(WindowState{})
	if got != (WindowState{}) {
		t.Fatalf("zero state should stay zero, got %+v", got)
	}
}

func TestSaveWindowState_AtomicRename(t *testing.T) {
	dir := withTempUserDataDir(t)

	if err := SaveWindowState(WindowState{Width: 1500, Height: 1000}); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	// The committed file should exist and parse cleanly.
	data, err := os.ReadFile(filepath.Join(dir, windowStateFile))
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var s WindowState
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if s.Width != 1500 || s.Height != 1000 {
		t.Fatalf("expected 1500x1000, got %dx%d", s.Width, s.Height)
	}

	// No leftover temp files should remain in the directory.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if name := e.Name(); name != windowStateFile {
			t.Fatalf("unexpected leftover file in user data dir: %s", name)
		}
	}
}

func TestSaveWindowState_Perm0600(t *testing.T) {
	dir := withTempUserDataDir(t)

	if err := SaveWindowState(WindowState{Width: 1280, Height: 800}); err != nil {
		t.Fatalf("SaveWindowState: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, windowStateFile))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	// Mask to just the permission bits.
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Fatalf("expected 0600 perms, got %o", perm)
	}
}
