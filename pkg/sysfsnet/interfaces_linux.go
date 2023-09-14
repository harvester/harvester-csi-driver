package sysfsnet

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"
)

var sysClassNet = "/sys/class/net"

type Interface struct {
	HardwareAddr net.HardwareAddr
}

func Interfaces() ([]Interface, error) {
	dents, err := os.ReadDir(sysClassNet)
	if err != nil {
		return nil, err
	}

	ifaces := make([]Interface, 0, 16)

	readMACFromFile := func(s string) (string, error) {
		f, err := os.Open(s)
		if os.IsNotExist(err) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		defer f.Close()

		r := bufio.NewScanner(bufio.NewReader(f))
		r.Split(bufio.ScanLines)
		_ = r.Scan()
		return r.Text(), nil
	}

	for _, dentry := range dents {
		hwText, err := readMACFromFile(filepath.Join(sysClassNet, dentry.Name(), "address"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}

		hwText = strings.TrimSpace(hwText)
		hw, err := net.ParseMAC(hwText)
		if err != nil {
			continue
		}

		ifaces = append(ifaces, Interface{HardwareAddr: hw})
	}

	return ifaces, nil
}
