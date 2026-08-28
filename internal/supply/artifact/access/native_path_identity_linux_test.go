//go:build linux

package access

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestLinuxPathComponentIdentityRequiresBirthTime(t *testing.T) {
	entry := nativeEntry{identity: nativeIdentity{
		device: 1,
		inode:  2,
		mode:   unix.S_IFDIR,
	}}
	stat := unix.Statx_t{
		Mask:   unix.STATX_MNT_ID,
		Mnt_id: 3,
	}

	_, err := nativePathComponentIdentityFromStatx(entry, stat)
	if !errors.Is(err, ErrNoFollowTraversalUnavailable) {
		t.Fatalf("missing Linux birth-time identity error = %v, want typed unavailable outcome", err)
	}
}

func TestLinuxPathComponentIdentityAcceptsMaskedEpochBirthTime(t *testing.T) {
	entry := nativeEntry{identity: nativeIdentity{
		device: 1,
		inode:  2,
		mode:   unix.S_IFDIR,
	}}
	stat := unix.Statx_t{
		Mask:   unix.STATX_MNT_ID | unix.STATX_BTIME,
		Mnt_id: 3,
	}

	identity, err := nativePathComponentIdentityFromStatx(entry, stat)
	if err != nil {
		t.Fatalf("masked zero birth-time identity: %v", err)
	}
	if identity.birthTimeSecond != 0 || identity.birthTimeNano != 0 {
		t.Fatalf(
			"masked epoch birth time = %d/%d, want 0/0",
			identity.birthTimeSecond,
			identity.birthTimeNano,
		)
	}
}
