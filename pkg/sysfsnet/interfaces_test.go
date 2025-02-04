package sysfsnet

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInterfaces(t *testing.T) {
	origDir := sysClassNet
	sysClassNet = t.TempDir()
	defer func() { sysClassNet = origDir }()

	dir := map[string]string{
		"eth0": "ce:ce:ce:ce:ce:ce",
		"lo":   "00:00:00:00:00:00",
	}

	for subdir, address := range dir {
		path := filepath.Join(sysClassNet, subdir, "address")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("os.MkdirAll %q want err=<nil>, got err=%v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(address), 0o644); err != nil {
			t.Fatalf("os.WriteFile %q want err=<nil>, got err=%v", path, err)
		}
	}

	// Interfaces() should follow symlinkDir.
	symlinkDir := filepath.Join(sysClassNet, "eth0_ln")
	if err := os.Symlink(filepath.Join(sysClassNet, "eth0"), symlinkDir); err != nil {
		t.Fatalf("os.Symlink %q want err=<nil>, got err=%v", symlinkDir, err)
	}

	// Interfaces() should skip non-directories like flatFile and symlinkFile.
	flatFile := filepath.Join(sysClassNet, "bonding_masters")
	if err := os.WriteFile(flatFile, []byte("bond1 bond2 bond3"), 0o644); err != nil {
		t.Fatalf("os.WriteFile %q want err=<nil>, got err=%v", flatFile, err)
	}

	symlinkFile := filepath.Join(sysClassNet, "bonding_masters_ln")
	if err := os.Symlink(flatFile, symlinkFile); err != nil {
		t.Fatalf("os.Symlink %q want err=<nil>, got err=%v", symlinkFile, err)
	}

	got, err := Interfaces()
	if err != nil {
		t.Fatalf("want err=<nil>, got err=%v", err)
	}

	want := []Interface{
		{HardwareAddr: []byte{0xce, 0xce, 0xce, 0xce, 0xce, 0xce}}, // from eth0
		{HardwareAddr: []byte{0xce, 0xce, 0xce, 0xce, 0xce, 0xce}}, // from eth0_ln
		{HardwareAddr: []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}},
	}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want MACs=%v, got MACs=%v", want, got)
	}
}
