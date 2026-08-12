//go:build darwin

package filesnapshot

import (
	"os"
	"syscall"
)

func regularFileObjectIdentity(info os.FileInfo) (fileObjectIdentity, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileObjectIdentity{}, false
	}
	return fileObjectIdentity{device: uint64(stat.Dev), inode: stat.Ino}, true
}
