package csi

import (
	"os"

	"golang.org/x/sys/unix"
)

const (
	deviceByIDDirectory = "/dev/disk/by-id/"
	driverName          = "driver.harvesterhci.io"
)

type volumeFilesystemStatistics struct {
	availableBytes int64
	totalBytes     int64
	usedBytes      int64

	availableInodes int64
	totalInodes     int64
	usedInodes      int64
}

// isBlockDevice return true if volumePath file is a block device, false otherwise.
func isBlockDevice(volumePath string) (bool, error) {
	var stat unix.Stat_t
	// See https://man7.org/linux/man-pages/man2/stat.2.html for details
	err := unix.Stat(volumePath, &stat)
	if err != nil {
		return false, err
	}

	// See https://man7.org/linux/man-pages/man7/inode.7.html for detail
	if (stat.Mode & unix.S_IFMT) == unix.S_IFBLK {
		return true, nil
	}

	return false, nil
}

func getFilesystemStatistics(volumePath string) (*volumeFilesystemStatistics, error) {
	var statfs unix.Statfs_t
	// See http://man7.org/linux/man-pages/man2/statfs.2.html for details.
	err := unix.Statfs(volumePath, &statfs)
	if err != nil {
		return nil, err
	}

	volStats := &volumeFilesystemStatistics{
		availableBytes: int64(statfs.Bavail) * int64(statfs.Bsize),
		totalBytes:     int64(statfs.Blocks) * int64(statfs.Bsize),
		usedBytes:      (int64(statfs.Blocks) - int64(statfs.Bfree)) * int64(statfs.Bsize),

		availableInodes: int64(statfs.Ffree),
		totalInodes:     int64(statfs.Files),
		usedInodes:      int64(statfs.Files) - int64(statfs.Ffree),
	}

	return volStats, nil
}

// makeDir creates a new directory.
// If pathname already exists as a directory, no error is returned.
// If pathname already exists as a file, an error is returned.
func makeDir(pathname string) error {
	err := os.MkdirAll(pathname, os.FileMode(0755))
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// makeFile creates an empty file.
// If pathname already exists, whether a file or directory, no error is returned.
func makeFile(pathname string) error {
	f, err := os.OpenFile(pathname, os.O_CREATE, os.FileMode(0644))
	if f != nil {
		f.Close()
	}
	if err != nil {
		if !os.IsExist(err) {
			return err
		}
	}
	return nil
}
