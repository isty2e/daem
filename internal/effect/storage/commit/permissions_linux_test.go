package commit

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestRestrictionAndReplacementLeaveLinuxAccessACLIntact(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writePermissionFixture(t, path, "unchanged", 0o644)

	// The named-user entry makes this an extended ACL even though stat reports 0644.
	const noID = ^uint32(0)
	acl := binary.LittleEndian.AppendUint32(nil, 2)
	for _, e := range []struct {
		tag, access uint16
		id          uint32
	}{
		{1, 6, noID}, {2, 4, uint32(os.Getuid()) ^ 1}, {4, 4, noID}, {16, 4, noID}, {32, 4, noID},
	} {
		acl = binary.LittleEndian.AppendUint16(acl, e.tag)
		acl = binary.LittleEndian.AppendUint16(acl, e.access)
		acl = binary.LittleEndian.AppendUint32(acl, e.id)
	}
	if err := unix.Setxattr(path, "system.posix_acl_access", acl, 0); errors.Is(err, unix.EOPNOTSUPP) {
		t.Skip("filesystem does not support POSIX ACLs")
	} else if err != nil {
		t.Fatal(err)
	}

	request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
	result, err := RestrictRootedFilePermissions(t.Context(), request)
	requirePermissionFailure(t, err, failureUnsupportedGuarantee)
	if result.Restricted() {
		t.Fatal("access ACL was modified")
	}
	replacement, err := NewFileReplacement(path, []byte("changed"), 0o600, captureIdentity(t, path))
	if err != nil {
		t.Fatal(err)
	}
	assertFailure(t, CommitFile(t.Context(), replacement), failureUnsupportedGuarantee, phaseCaptureMetadata)
	assertFile(t, path, "unchanged", 0o644)
	value := make([]byte, len(acl))
	n, err := unix.Getxattr(path, "system.posix_acl_access", value)
	if err != nil || string(value[:n]) != string(acl) {
		t.Fatalf("original ACL did not survive: %v", err)
	}
}
