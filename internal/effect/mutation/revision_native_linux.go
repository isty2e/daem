//go:build linux

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
			device: uint64(stat.Dev), inode: stat.Ino, mode: stat.Mode, size: stat.Size,
			mtimeSec: stat.Mtim.Sec, mtimeNsec: stat.Mtim.Nsec,
			changeSec: stat.Ctim.Sec, changeNsec: stat.Ctim.Nsec,
		},
		mode: stat.Mode,
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
			device: uint64(stat.Dev), inode: stat.Ino, mode: stat.Mode, size: stat.Size,
			mtimeSec: stat.Mtim.Sec, mtimeNsec: stat.Mtim.Nsec,
			changeSec: stat.Ctim.Sec, changeNsec: stat.Ctim.Nsec,
		},
		mode: stat.Mode,
		size: stat.Size,
	}, true
}

func (identity revisionNativeIdentity) equal(other revisionNativeIdentity) bool {
	return identity == other
}
