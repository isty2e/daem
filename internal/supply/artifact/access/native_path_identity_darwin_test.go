//go:build darwin

package access

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestNativePathComponentIdentityFromStatRejectsUnavailableIncarnation(t *testing.T) {
	entry := darwinPathIdentityTestEntry()
	stat := unix.Stat_t{}
	filesystem := darwinPathIdentityTestFilesystem()

	_, err := nativePathComponentIdentityFromStat(entry, stat, filesystem)
	if !errors.Is(err, ErrNoFollowTraversalUnavailable) {
		t.Fatalf("missing Darwin incarnation identity error = %v, want typed unavailable outcome", err)
	}
}

func TestNativePathComponentIdentityFromStatAcceptsAvailableIncarnation(t *testing.T) {
	tests := []struct {
		name string
		stat unix.Stat_t
	}{
		{name: "generation", stat: unix.Stat_t{Gen: 7}},
		{name: "birth time", stat: unix.Stat_t{Btim: unix.Timespec{Sec: 8, Nsec: 9}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, err := nativePathComponentIdentityFromStat(
				darwinPathIdentityTestEntry(),
				test.stat,
				darwinPathIdentityTestFilesystem(),
			)
			if err != nil {
				t.Fatal(err)
			}
			if identity.generation != uint64(test.stat.Gen) ||
				identity.birthTimeSecond != test.stat.Btim.Sec ||
				identity.birthTimeNano != test.stat.Btim.Nsec {
				t.Fatalf("Darwin incarnation identity = %#v, want stat values %#v", identity, test.stat)
			}
		})
	}
}

func darwinPathIdentityTestEntry() nativeEntry {
	return nativeEntry{identity: nativeIdentity{
		device: 1,
		inode:  2,
		mode:   unix.S_IFDIR,
	}}
}

func darwinPathIdentityTestFilesystem() unix.Statfs_t {
	return unix.Statfs_t{Fsid: unix.Fsid{Val: [2]int32{3, 4}}}
}
