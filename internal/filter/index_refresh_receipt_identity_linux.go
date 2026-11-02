//go:build linux

package filter

import (
	"os"
	"syscall"
)

func fileIdentity(info os.FileInfo) (device, inode uint64, changedNS int64, ok bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, 0, 0, false
	}
	return uint64(stat.Dev), stat.Ino, stat.Ctim.Sec*1e9 + stat.Ctim.Nsec, true
}
