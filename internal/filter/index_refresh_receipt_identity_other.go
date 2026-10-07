//go:build !linux && !darwin

package filter

import "os"

// Platforms without a supported ctime/dev/inode tuple keep the hash-based
// refresh path instead of accepting a weaker receipt.
func fileIdentity(os.FileInfo) (device, inode uint64, changedNS int64, ok bool) {
	return 0, 0, 0, false
}
