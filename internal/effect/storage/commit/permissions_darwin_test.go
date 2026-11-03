package commit

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPermissionRestrictionPreservesDarwinExtendedAttributes(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writePermissionFixture(t, path, "malformed [", 0o644)
	if err := unix.Setxattr(path, "com.daem.permission-test", []byte("retained"), 0); err != nil {
		t.Fatal(err)
	}
	request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
	result, err := RestrictRootedFilePermissions(t.Context(), request)
	if err != nil || !result.Expanded() || !result.Restricted() {
		t.Fatal("supported xattrs blocked or disappeared during restriction", err)
	}
	var value [32]byte
	length, err := unix.Getxattr(path, "com.daem.permission-test", value[:])
	if err != nil || string(value[:length]) != "retained" {
		t.Fatal("permission restriction changed unrelated xattrs", err)
	}
}

func TestPermissionRestrictionRefusesDarwinACLAndFileFlags(t *testing.T) {
	for _, mode := range []string{"ACL", "flag"} {
		t.Run(mode, func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			writePermissionFixture(t, path, "unchanged", 0o644)
			if mode == "ACL" {
				if err := exec.Command("/bin/chmod", "+a", "everyone deny delete", path).Run(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = exec.Command("/bin/chmod", "-N", path).Run() })
			} else {
				if err := unix.Chflags(path, unix.UF_NODUMP); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = unix.Chflags(path, 0) })
			}
			request := permissionRequestForTest(t, captureRootForCommitTest(t, root), "config", 0o600)
			result, err := RestrictRootedFilePermissions(t.Context(), request)
			requirePermissionFailure(t, err, failureUnsupportedGuarantee)
			if result.Restricted() {
				t.Fatal("unqualified metadata reached chmod")
			}
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != 0o644 {
				t.Fatal("metadata refusal changed permissions", err)
			}
			assertFileContent(t, path, "unchanged")
		})
	}
}
