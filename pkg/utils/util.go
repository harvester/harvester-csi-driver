// Vendored from https://github.com/longhorn/longhorn-manager/blob/v1.5.4/util/util.go
// under the Apache-2.0 license.
package utils

const (
	SizeAlignment     = 2 * 1024 * 1024
	MinimalVolumeSize = 10 * 1024 * 1024
)

func RoundUpSize(size int64) int64 {
	if size <= 0 {
		return SizeAlignment
	}
	r := size % SizeAlignment
	if r == 0 {
		return size
	}
	return size - r + SizeAlignment
}
