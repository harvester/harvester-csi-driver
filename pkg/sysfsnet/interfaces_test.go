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

	nonDirPath := filepath.Join(sysClassNet, "bonding_masters")
	if err := os.WriteFile(nonDirPath, []byte(""), 0o644); err != nil {
		t.Fatalf("os.WriteFile %q want err=<nil>, got err=%v", nonDirPath, err)
	}

	ifaces, err := Interfaces()
	if err != nil {
		t.Fatalf("want err=<nil>, got err=%v", err)
	}

	got := make(map[string]struct{})
	for _, iface := range ifaces {
		got[iface.HardwareAddr.String()] = struct{}{}
	}

	want := map[string]struct{}{
		"ce:ce:ce:ce:ce:ce": {},
		"00:00:00:00:00:00": {},
	}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want MACs=%v, got MACs=%v", want, got)
	}

	// Ensure non-directory entry is ignored
	if _, exists := got[""]; exists {
		t.Fatalf("non-directory entry was incorrectly processed")
	}
}
