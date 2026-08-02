//go:build linux

package filesnapshot

import (
	"os"
	"syscall"
)

func fileChangeVersion(info os.FileInfo) (changeVersion, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return changeVersion{}, false
	}
	return changeVersion{
		seconds:     stat.Ctim.Sec,
		nanoseconds: stat.Ctim.Nsec,
	}, true
}
