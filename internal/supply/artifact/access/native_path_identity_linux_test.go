//go:build linux

package access

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxPathComponentIdentityNeedsIncarnationAndMountEvidence(t *testing.T) {
	entry := nativeEntry{identity: nativeIdentity{device: 1, inode: 2, mode: unix.S_IFDIR}}
	handle := unix.NewFileHandle(11, []byte("opaque"))
	empty := unix.NewFileHandle(11, nil)
	for _, test := range []struct {
		name   string
		mask   uint32
		handle *unix.FileHandle
	}{
		{"no incarnation", unix.STATX_MNT_ID, nil},
		{"empty handle", unix.STATX_MNT_ID, &empty},
		{"no mount with birth time", unix.STATX_BTIME, nil},
		{"no mount with handle", 0, &handle},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := nativePathComponentIdentityFromStatx(entry, unix.Statx_t{Mask: test.mask}, test.handle)
			if !errors.Is(err, ErrNoFollowTraversalUnavailable) {
				t.Fatalf("identity error = %v, want typed unavailable outcome", err)
			}
		})
	}
}

func TestLinuxPathComponentIdentityAcceptsMaskedEpochBirthTime(t *testing.T) {
	entry := nativeEntry{identity: nativeIdentity{device: 1, inode: 2, mode: unix.S_IFDIR}}
	stat := unix.Statx_t{Mask: unix.STATX_MNT_ID | unix.STATX_BTIME, Mnt_id: 3}
	identity, err := nativePathComponentIdentityFromStatx(entry, stat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if identity.birthTimeSecond != 0 || identity.birthTimeNano != 0 {
		t.Fatalf("masked epoch birth time = %d/%d, want 0/0", identity.birthTimeSecond, identity.birthTimeNano)
	}
}

func TestLinuxPathComponentWitnessBindsOpaqueHandleTypeAndBytes(t *testing.T) {
	entry := nativeEntry{identity: nativeIdentity{device: 1, inode: 2, mode: unix.S_IFDIR}}
	witness := func(stat unix.Statx_t, kind int32, value string) nativePathWitness {
		t.Helper()
		handle := unix.NewFileHandle(kind, []byte(value))
		identity, err := nativePathComponentIdentityFromStatx(entry, stat, &handle)
		if err != nil {
			t.Fatal(err)
		}
		builder := newNativePathWitnessBuilder()
		builder.append(identity)
		return builder.finish()
	}
	stat := unix.Statx_t{Mask: unix.STATX_MNT_ID, Mnt_id: 3}
	base := witness(stat, 11, "opaque")
	if base != witness(stat, 11, "opaque") {
		t.Fatal("same handle produced a different witness")
	}
	if base == witness(stat, 12, "opaque") || base == witness(stat, 11, "recreated") {
		t.Fatal("handle type or bytes were omitted from the witness")
	}
	stat.Mask |= unix.STATX_BTIME
	if base == witness(stat, 11, "opaque") {
		t.Fatal("file handle and birth-time identity modes alias")
	}
}
