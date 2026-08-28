//go:build darwin || linux

package access

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"io/fs"

	"github.com/isty2e/daem/internal/supply/artifact"
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

type nativeExactNameBinding struct {
	identity nativeIdentity
}

func (binding nativeExactNameBinding) sameBinding(other nativeExactNameBinding) bool {
	return binding.identity.sameBinding(other.identity)
}

func (binding nativeExactNameBinding) matches(entry nativeEntry) bool {
	return binding.identity.sameBinding(entry.identity)
}

type nativeMountIdentity struct {
	first  uint64
	second uint64
}

type nativePathComponentIdentity struct {
	device          uint64
	inode           uint64
	kind            uint32
	generation      uint64
	birthTimeSecond int64
	birthTimeNano   int64
	mount           nativeMountIdentity
}

type nativePathWitness struct {
	digest     [sha256.Size]byte
	components uint32
}

func (witness nativePathWitness) valid() bool {
	return witness.components != 0 && witness.digest != [sha256.Size]byte{}
}

func (witness nativePathWitness) require(current nativePathWitness, subject string) error {
	if witness.valid() && witness != current {
		return fmt.Errorf("artifact access %s authority changed", subject)
	}
	return nil
}

type nativePathWitnessBuilder struct {
	hash       hash.Hash
	components uint32
}

func newNativePathWitnessBuilder() *nativePathWitnessBuilder {
	builder := &nativePathWitnessBuilder{hash: sha256.New()}
	_, _ = builder.hash.Write([]byte("daem-artifact-native-path-v1\x00"))
	return builder
}

func (builder *nativePathWitnessBuilder) append(identity nativePathComponentIdentity) {
	values := [...]uint64{
		identity.device,
		identity.inode,
		uint64(identity.kind),
		identity.generation,
		uint64(identity.birthTimeSecond),
		uint64(identity.birthTimeNano),
		identity.mount.first,
		identity.mount.second,
	}
	var encoded [8]byte
	for _, value := range values {
		binary.BigEndian.PutUint64(encoded[:], value)
		_, _ = builder.hash.Write(encoded[:])
	}
	builder.components++
}

func (builder *nativePathWitnessBuilder) finish() nativePathWitness {
	var digest [sha256.Size]byte
	copy(digest[:], builder.hash.Sum(nil))
	return nativePathWitness{digest: digest, components: builder.components}
}

type directoryListingIdentity struct {
	native   nativeIdentity
	relative nativePathWitness
}

func newDirectoryListingWitness(
	identity nativeIdentity,
	relative nativePathWitness,
) DirectoryListingWitness {
	return DirectoryListingWitness{identity: directoryListingIdentity{
		native:   identity,
		relative: relative,
	}}
}

func (identity directoryListingIdentity) equal(other directoryListingIdentity) bool {
	return identity.native.equal(other.native) && identity.relative == other.relative
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
	path      string
	kind      artifact.ArtifactKind
	name      string
	parentFD  int
	entry     nativeEntry
	authority nativePathWitness
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
