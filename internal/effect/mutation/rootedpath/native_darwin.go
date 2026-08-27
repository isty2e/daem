//go:build darwin

package rootedpath

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// darwinOpenSearch mirrors O_SEARCH from the macOS SDK; x/sys does not expose it.
const darwinOpenSearch = 0x40000000 | unix.O_DIRECTORY

func capturedDirectoryOpenFlags(searchOnly bool) int {
	if searchOnly {
		return darwinOpenSearch | unix.O_CLOEXEC | unix.O_NOFOLLOW
	}
	return unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW
}

func nativeObjectToken(fd int, device uint64, inode uint64) (identityToken, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return identityToken{}, err
	}
	return identityTokenFromValues(
		"darwin-rooted-path-object-v1",
		device,
		inode,
		uint64(stat.Gen),
		uint64(stat.Btim.Sec),
		uint64(stat.Btim.Nsec),
	), nil
}

func nativeMountToken(fd int) (identityToken, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		return identityToken{}, err
	}
	return identityTokenFromValues(
		"darwin-rooted-path-mount-v1",
		uint64(uint32(stat.Fsid.Val[0])),
		uint64(uint32(stat.Fsid.Val[1])),
	), nil
}

type darwinMountAttributeBuffer struct {
	length uint32
	fsid   [2]int32
}

func nativeMountTokenAt(parentFD int, name string) (identityToken, error) {
	namePointer, err := unix.BytePtrFromString(name)
	if err != nil {
		return identityToken{}, err
	}
	attributes := unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_FSID,
	}
	var buffer darwinMountAttributeBuffer
	_, _, errno := unix.Syscall6(
		unix.SYS_GETATTRLISTAT,
		uintptr(parentFD),
		uintptr(unsafe.Pointer(namePointer)),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer)),
		unsafe.Sizeof(buffer),
		unix.FSOPT_NOFOLLOW,
	)
	runtime.KeepAlive(namePointer)
	runtime.KeepAlive(&attributes)
	runtime.KeepAlive(&buffer)
	if errno != 0 {
		return identityToken{}, errno
	}
	if buffer.length != uint32(unsafe.Sizeof(buffer)) {
		return identityToken{}, fmt.Errorf("inspect entry mount: invalid attribute size %d", buffer.length)
	}
	return identityTokenFromValues(
		"darwin-rooted-path-mount-v1",
		uint64(uint32(buffer.fsid[0])),
		uint64(uint32(buffer.fsid[1])),
	), nil
}

func nativeRecoveryMountToken(fd int) (identityToken, error) {
	return nativeMountToken(fd)
}
