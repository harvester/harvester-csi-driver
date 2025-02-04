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
		"eth0":   "ce:ce:ce:ce:ce:ce",
		"lo":     "00:00:00:00:00:00",
		"bond0":  "aa:00:00:00:00:11",
		"dummy0": "invalid", // invalid MAC address should be skipped
		"dummy1": "",        // empty MAC address should be skipped
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

	nonDir := map[string]string{
		"bonding_masters": "+bond0", // non-directory should be skipped
	}

	for subdir, content := range nonDir {
		path := filepath.Join(sysClassNet, subdir)
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("os.WriteFile %q want err=<nil>, got err=%v", path, err)
		}
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
		"aa:00:00:00:00:11": {},
	}

	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want MACs=%v, got MACs=%v", want, got)
	}
}
