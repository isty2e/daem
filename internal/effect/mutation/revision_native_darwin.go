//go:build darwin

package mutation

import (
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type revisionNativeIdentity struct {
	device     uint64
	inode      uint64
	mode       uint32
	size       int64
	mtimeSec   int64
	mtimeNsec  int64
	changeSec  int64
	changeNsec int64
}

func revisionNativeEntryFromStat(stat *unix.Stat_t) revisionNativeEntry {
	return revisionNativeEntry{
		identity: revisionNativeIdentity{
			device: uint64(stat.Dev), inode: stat.Ino, mode: uint32(stat.Mode), size: stat.Size,
			mtimeSec: stat.Mtim.Sec, mtimeNsec: stat.Mtim.Nsec,
			changeSec: stat.Ctim.Sec, changeNsec: stat.Ctim.Nsec,
		},
		mode: uint32(stat.Mode),
		size: stat.Size,
	}
}

func revisionNativeEntryFromFileInfo(info os.FileInfo) (revisionNativeEntry, bool) {
	if info == nil {
		return revisionNativeEntry{}, false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat == nil {
		return revisionNativeEntry{}, false
	}
	return revisionNativeEntry{
		identity: revisionNativeIdentity{
			device: uint64(stat.Dev), inode: stat.Ino, mode: uint32(stat.Mode), size: stat.Size,
			mtimeSec: stat.Mtimespec.Sec, mtimeNsec: stat.Mtimespec.Nsec,
			changeSec: stat.Ctimespec.Sec, changeNsec: stat.Ctimespec.Nsec,
		},
		mode: uint32(stat.Mode),
		size: stat.Size,
	}, true
}

func (identity revisionNativeIdentity) equal(other revisionNativeIdentity) bool {
	return identity == other
}
