//go:build darwin || linux

package access

import (
	"io/fs"

	"golang.org/x/sys/unix"
)

type nativeKind uint8

const (
	nativeKindUnsupported nativeKind = iota
	nativeKindFile
	nativeKindDirectory
	nativeKindSymlink
)

type nativeIdentity struct {
	device           uint64
	inode            uint64
	changeTimeSecond int64
	changeTimeNano   int64
	mode             uint32
	size             int64
}

type directoryListingIdentity struct {
	native nativeIdentity
}

func newDirectoryListingWitness(identity nativeIdentity) DirectoryListingWitness {
	return DirectoryListingWitness{identity: directoryListingIdentity{native: identity}}
}

func (identity directoryListingIdentity) equal(other directoryListingIdentity) bool {
	return identity.native.equal(other.native)
}

func (identity nativeIdentity) equal(other nativeIdentity) bool {
	return identity.device == other.device &&
		identity.inode == other.inode &&
		identity.changeTimeSecond == other.changeTimeSecond &&
		identity.changeTimeNano == other.changeTimeNano &&
		identity.mode == other.mode &&
		identity.size == other.size
}

func (identity nativeIdentity) sameBinding(other nativeIdentity) bool {
	return identity.device == other.device &&
		identity.inode == other.inode &&
		identity.mode&unix.S_IFMT == other.mode&unix.S_IFMT
}

type nativeEntry struct {
	fd       int
	kind     nativeKind
	mode     fs.FileMode
	size     int64
	identity nativeIdentity
}

type nativeRoot struct {
	name     string
	parentFD int
	entry    nativeEntry
}

func nativeEntryFromStat(fd int, stat *unix.Stat_t) nativeEntry {
	return nativeEntry{
		fd:       fd,
		kind:     nativeKindFromMode(uint32(stat.Mode)),
		mode:     fs.FileMode(stat.Mode & 0o777),
		size:     stat.Size,
		identity: nativeIdentityFromStat(stat),
	}
}

func nativeIdentityFromStat(stat *unix.Stat_t) nativeIdentity {
	return nativeIdentity{
		device:           uint64(stat.Dev),
		inode:            uint64(stat.Ino),
		changeTimeSecond: int64(stat.Ctim.Sec),
		changeTimeNano:   int64(stat.Ctim.Nsec),
		mode:             uint32(stat.Mode),
		size:             stat.Size,
	}
}

func nativeKindFromMode(mode uint32) nativeKind {
	switch mode & unix.S_IFMT {
	case unix.S_IFREG:
		return nativeKindFile
	case unix.S_IFDIR:
		return nativeKindDirectory
	case unix.S_IFLNK:
		return nativeKindSymlink
	default:
		return nativeKindUnsupported
	}
}

func publicEntryKind(kind nativeKind) EntryKind {
	switch kind {
	case nativeKindFile:
		return EntryKindFile
	case nativeKindDirectory:
		return EntryKindDirectory
	case nativeKindSymlink:
		return EntryKindSymlink
	default:
		return EntryKindUnsupported
	}
}
