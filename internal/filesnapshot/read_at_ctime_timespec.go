//go:build darwin || freebsd || netbsd

package filesnapshot

import (
	"os"
	"syscall"
)

func fileInfoAtCtime(info os.FileInfo) changeVersion {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return changeVersion{}
	}
	return changeVersion{
		seconds:     stat.Ctimespec.Sec,
		nanoseconds: stat.Ctimespec.Nsec,
	}
}
