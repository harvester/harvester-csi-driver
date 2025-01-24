package sysfsnet

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
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

		entryPath := filepath.Join(sysClassNet, dentry.Name())

		// os.Stat to follow symlinks and return the info of the target
		info, err := os.Stat(entryPath)
		if err != nil {
			logrus.Warnf("Unable to stat %s: %v", entryPath, err)
			continue
		}

		// os.FileInfo.IsDir to check if the entry is a directory
		if !info.IsDir() {
			logrus.Warnf("Skipping non-directory entry %s", entryPath)
			continue
		}

		hwText, err := readMACFromFile(filepath.Join(entryPath, "address"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}

		hwText = strings.TrimSpace(hwText)
		hw, err := net.ParseMAC(hwText)
		if err != nil {
			logrus.Warnf("Unable to parse MAC %s: %v", hwText, err)
			continue
		}

		ifaces = append(ifaces, Interface{HardwareAddr: hw})
	}

	return ifaces, nil
}
