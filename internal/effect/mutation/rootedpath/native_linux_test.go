//go:build linux

package rootedpath

import (
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

func TestParseLinuxBootIDAcceptsOnlyCanonicalUUID(t *testing.T) {
	const canonical = "12345678-90ab-cdef-1234-567890abcdef"
	withoutNewline, err := parseLinuxBootID(canonical)
	if err != nil {
		t.Fatalf("parseLinuxBootID returned error: %v", err)
	}
	withNewline, err := parseLinuxBootID(canonical + "\n")
	if err != nil {
		t.Fatalf("parseLinuxBootID with newline returned error: %v", err)
	}
	if withoutNewline != withNewline {
		t.Fatal("optional terminal newline changed Linux boot identity")
	}

	for _, invalid := range []string{
		"",
		canonical + "\n\n",
		"12345678-90AB-CDEF-1234-567890ABCDEF",
		"1234567890ab-cdef-1234-567890abcdef",
		"12345678-90ab-cdef-1234-567890abcdeg",
		" 12345678-90ab-cdef-1234-567890abcdef",
	} {
		if _, err := parseLinuxBootID(invalid); err == nil {
			t.Fatalf("parseLinuxBootID(%q) succeeded", invalid)
		}
	}
}

func TestLinuxRecoveryMountTokenBindsMountAndBootIdentity(t *testing.T) {
	firstBoot, err := parseLinuxBootID("12345678-90ab-cdef-1234-567890abcdef")
	if err != nil {
		t.Fatalf("parse first boot ID: %v", err)
	}
	secondBoot, err := parseLinuxBootID("22345678-90ab-cdef-1234-567890abcdef")
	if err != nil {
		t.Fatalf("parse second boot ID: %v", err)
	}

	base := linuxRecoveryMountToken(41, firstBoot)
	if base != linuxRecoveryMountToken(41, firstBoot) {
		t.Fatal("same mount and boot produced different recovery tokens")
	}
	if base == linuxRecoveryMountToken(42, firstBoot) {
		t.Fatal("different unique mount IDs produced the same recovery token")
	}
	if base == linuxRecoveryMountToken(41, secondBoot) {
		t.Fatal("different boot IDs produced the same recovery token")
	}
}

func TestLinuxBootIDCacheRetriesFailureAndCachesOnlySuccess(t *testing.T) {
	want, err := parseLinuxBootID("12345678-90ab-cdef-1234-567890abcdef")
	if err != nil {
		t.Fatal(err)
	}
	transient := errors.New("transient boot identity read failure")
	calls := 0
	cache := linuxBootIDCache{read: func() (linuxBootID, error) {
		calls++
		if calls == 1 {
			return linuxBootID{}, transient
		}
		return want, nil
	}}

	if _, err := cache.current(); !errors.Is(err, transient) {
		t.Fatalf("first current error = %v, want transient failure", err)
	}
	got, err := cache.current()
	if err != nil {
		t.Fatalf("second current returned error: %v", err)
	}
	if got != want {
		t.Fatalf("second current = %#v, want %#v", got, want)
	}
	got, err = cache.current()
	if err != nil || got != want {
		t.Fatalf("cached current = %#v, %v; want %#v, nil", got, err, want)
	}
	if calls != 2 {
		t.Fatalf("boot identity reader calls = %d, want 2", calls)
	}
}

func TestLinuxRecoveryMountEvidenceModesDoNotAlias(t *testing.T) {
	boot := linuxBootID{high: 1, low: 2}
	capture := func(mask uint32, mount uint64, boot linuxBootID) identityToken {
		t.Helper()
		token, err := linuxRecoveryMountTokenFromStatx(unix.Statx_t{Mask: mask, Mnt_id: mount}, boot)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	unique := capture(unix.STATX_MNT_ID_UNIQUE, 41, boot)
	if unique != linuxRecoveryMountToken(41, boot) {
		t.Fatal("unique-mount evidence changed the existing journal token")
	}
	legacy := capture(unix.STATX_MNT_ID, 41, boot)
	if legacy == unique {
		t.Fatal("legacy mount evidence aliases unique-mount evidence")
	}
	if legacy != capture(unix.STATX_MNT_ID, 41, boot) {
		t.Fatal("legacy mount token is unstable")
	}
	if legacy == capture(unix.STATX_MNT_ID, 42, boot) ||
		legacy == capture(unix.STATX_MNT_ID, 41, linuxBootID{high: 1, low: 3}) {
		t.Fatal("legacy mount token omitted mount or boot evidence")
	}
	if _, err := linuxRecoveryMountTokenFromStatx(unix.Statx_t{}, boot); !errors.Is(err, errMountIdentityUnsupported) {
		t.Fatalf("missing mount error = %v, want unsupported", err)
	}
}

func TestNativeRecoveryMountTokenSurvivesDescriptorReopen(t *testing.T) {
	capture := func() identityToken {
		t.Helper()
		root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(root)
		token, err := nativeRecoveryMountToken(root)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	if capture() != capture() {
		t.Fatal("reopening the same mount changed recovery evidence")
	}
	if _, err := nativeRecoveryMountToken(-1); !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid descriptor error = %v, want EBADF", err)
	}
}

func TestLinuxRecoveryProvenanceRefusesCrossSchemeRecovery(t *testing.T) {
	for _, mask := range []uint32{unix.STATX_MNT_ID_UNIQUE, unix.STATX_MNT_ID} {
		authority := provenanceTestAuthority(t)
		token, err := linuxRecoveryMountTokenFromStatx(unix.Statx_t{Mask: mask, Mnt_id: 41}, linuxBootID{high: 1, low: 2})
		if err != nil {
			t.Fatal(err)
		}
		authority.mount.recovery = availableRecoveryMountEvidence(token)
		provenance, err := authority.Provenance()
		if err != nil {
			t.Fatal(err)
		}
		stored, err := NewAuthorityProvenance(provenance.PhysicalRoot(), provenance.ObjectFingerprint(), provenance.MountFingerprint())
		if err != nil {
			t.Fatal(err)
		}
		if err := stored.Match(authority); err != nil {
			t.Fatal(err)
		}
		otherMask := uint32(unix.STATX_MNT_ID_UNIQUE|unix.STATX_MNT_ID) ^ mask
		otherToken, err := linuxRecoveryMountTokenFromStatx(unix.Statx_t{Mask: otherMask, Mnt_id: 41}, linuxBootID{high: 1, low: 2})
		if err != nil {
			t.Fatal(err)
		}
		authority.mount.recovery = availableRecoveryMountEvidence(otherToken)
		if err := stored.Match(authority); !hasFailureKind(err, FailureMountChanged) {
			t.Fatalf("cross-scheme recovery error = %v, want mount mismatch", err)
		}
	}
}
