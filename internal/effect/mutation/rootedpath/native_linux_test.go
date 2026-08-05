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

func TestLinuxUniqueMountIDRejectsReusableMountObservation(t *testing.T) {
	if _, err := linuxUniqueMountID(unix.Statx_t{
		Mask:   unix.STATX_MNT_ID,
		Mnt_id: 41,
	}); !errors.Is(err, errMountIdentityUnsupported) {
		t.Fatalf("linuxUniqueMountID error = %v, want unsupported", err)
	}
	mountID, err := linuxUniqueMountID(unix.Statx_t{
		Mask:   unix.STATX_MNT_ID_UNIQUE,
		Mnt_id: 42,
	})
	if err != nil {
		t.Fatalf("linuxUniqueMountID returned error: %v", err)
	}
	if mountID != 42 {
		t.Fatalf("linuxUniqueMountID = %d, want 42", mountID)
	}
}

func TestNativeRecoveryMountTokenRequiresUniqueMountIDSupport(t *testing.T) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		t.Fatalf("open filesystem root: %v", err)
	}
	defer unix.Close(root)

	stat, err := statxDescriptor(root, unix.STATX_MNT_ID_UNIQUE)
	if err != nil {
		if errors.Is(err, errMountIdentityUnsupported) {
			t.Fatalf("admitted Linux runtime lacks STATX_MNT_ID_UNIQUE: %v", err)
		}
		t.Fatalf("statx unique mount identity: %v", err)
	}
	if stat.Mask&unix.STATX_MNT_ID_UNIQUE == 0 {
		t.Fatal("admitted Linux runtime did not return STATX_MNT_ID_UNIQUE")
	}
	if _, err := nativeRecoveryMountToken(root); err != nil {
		t.Fatalf("nativeRecoveryMountToken returned error: %v", err)
	}
}
