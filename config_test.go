package sled

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cfg.sled")
	db, err := DefaultConfig().
		Path(path).
		SyncWrites(false).
		FileMode(0o600).
		Open()
	if err != nil {
		t.Fatalf("Config.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok, _ := db.Get([]byte("k")); !ok || string(v) != "v" {
		t.Fatalf("Get = %q,%v; want v,true", v, ok)
	}
	if db.opts.syncWrites {
		t.Errorf("SyncWrites(false) not applied")
	}
	if db.opts.fileMode != 0o600 {
		t.Errorf("FileMode not applied: %o", db.opts.fileMode)
	}
}

func TestConfigNoPath(t *testing.T) {
	if _, err := DefaultConfig().Open(); err != ErrNoPath {
		t.Errorf("Open without path = %v; want ErrNoPath", err)
	}
}

func TestConfigTemporary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "temp.sled")
	db, err := DefaultConfig().Path(path).Temporary(true).Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = db.Set([]byte("k"), []byte("v"))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("log file should exist while open: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary DB left its file behind: err=%v", err)
	}
}

func TestWithTemporaryOption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wt.sled")
	db, err := Open(path, WithTemporary(true))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = db.Set([]byte("k"), []byte("v"))
	_ = db.Close()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("WithTemporary did not remove file: %v", err)
	}
}
