package utils

import (
	"fmt"

	"golang.org/x/mod/semver"
)

// IsVersionAtLeast checks if a version string (e.g., "v1.8.0", "v1.7.2-rc1")
// is at least the given major.minor version.
// Returns true for versions not starting with "v" or invalid semver (assumes latest).
func IsVersionAtLeast(version string, major, minor int) bool {
	if !semver.IsValid(version) {
		return true
	}

	target := fmt.Sprintf("v%d.%d.0-0", major, minor)
	return semver.Compare(version, target) >= 0
}
