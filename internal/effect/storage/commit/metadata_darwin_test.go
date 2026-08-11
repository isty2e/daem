//go:build darwin

package commit

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"golang.org/x/sys/unix"
)

func TestCommitFilePreservesExtendedAttributes(t *testing.T) {
	root := canonicalTempDir(t)
	target := filepath.Join(root, "state.json")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := unix.Setxattr(target, "com.daem.test", []byte("value"), 0); err != nil {
		t.Fatalf("Setxattr returned error: %v", err)
	}
	request, err := NewFileReplacement(target, []byte("after"), 0o600, captureIdentity(t, target))
	if err != nil {
		t.Fatalf("NewFileReplacement returned error: %v", err)
	}
	if err := CommitFile(context.Background(), request); err != nil {
		t.Fatalf("CommitFile returned error: %v", err)
	}
	buffer := make([]byte, 16)
	size, err := unix.Getxattr(target, "com.daem.test", buffer)
	if err != nil {
		t.Fatalf("Getxattr returned error: %v", err)
	}
	if got := string(buffer[:size]); got != "value" {
		t.Fatalf("xattr = %q, want value", got)
	}
}

func TestCommitFileRejectsACLsAndFlags(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, string)
		cleanup func(string)
	}{
		{
			name: "ACL",
			prepare: func(t *testing.T, path string) {
				if output, err := exec.Command("chmod", "+a", "everyone deny delete", path).CombinedOutput(); err != nil {
					t.Fatalf("chmod +a returned error: %v: %s", err, output)
				}
			},
			cleanup: func(path string) { _ = exec.Command("chmod", "-N", path).Run() },
		},
		{
			name: "file flag",
			prepare: func(t *testing.T, path string) {
				if err := unix.Chflags(path, unix.UF_NODUMP); err != nil {
					t.Fatalf("Chflags returned error: %v", err)
				}
			},
			cleanup: func(path string) { _ = unix.Chflags(path, 0) },
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			target := filepath.Join(root, "state.json")
			if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
				t.Fatalf("WriteFile returned error: %v", err)
			}
			test.prepare(t, target)
			if test.cleanup != nil {
				defer test.cleanup(target)
			}
			request, err := NewFileReplacement(target, []byte("after"), 0o600, captureIdentity(t, target))
			if err != nil {
				t.Fatalf("NewFileReplacement returned error: %v", err)
			}
			assertFailure(t, CommitFile(context.Background(), request), failureUnsupportedGuarantee, phaseCaptureMetadata)
			assertFile(t, target, "before", 0o600)
		})
	}
}

func TestPreparedMetadataVerificationRejectsInheritedACL(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "prepared")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if output, err := exec.Command("chmod", "+a", "everyone deny delete", path).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a returned error: %v: %s", err, output)
	}
	defer exec.Command("chmod", "-N", path).Run()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer file.Close()
	metadata := preservedMetadata{xattrs: make(map[string][]byte)}
	if err := verifyPreservedMetadata(int(file.Fd()), metadata); !isUnsupported(err) {
		t.Fatalf("verifyPreservedMetadata error = %v, want unsupported", err)
	}
}

func TestCommitFileRejectsInheritedACLOnCreatedAncestor(t *testing.T) {
	root := canonicalTempDir(t)
	if output, err := exec.Command(
		"chmod",
		"+a",
		"everyone deny delete_child,file_inherit,directory_inherit",
		root,
	).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a returned error: %v: %s", err, output)
	}
	defer exec.Command("chmod", "-N", root).Run()
	target := filepath.Join(root, "created", "state.json")
	request, err := NewFileCreate(target, nil, 0o600)
	if err != nil {
		t.Fatalf("NewFileCreate returned error: %v", err)
	}
	failure := assertFailure(
		t,
		CommitFile(context.Background(), request),
		failureUnsupportedGuarantee,
		phaseCreateAncestors,
	)
	residue := failure.retainedResidue()
	if len(residue) != 1 || filepath.Dir(residue[0]) != root ||
		!strings.HasPrefix(filepath.Base(residue[0]), temporaryPrefix) {
		t.Fatalf("inherited ACL residue = %v, want one unpublished ancestor stage", residue)
	}
	if _, err := os.Lstat(filepath.Join(root, "created")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("final ancestor was published despite inherited ACL: %v", err)
	}
	if info, err := os.Stat(residue[0]); err != nil || !info.IsDir() {
		t.Fatalf("reported staging residue is not retained: info=%v err=%v", info, err)
	}
}

func TestPrepareRootedTreeRejectsInheritedACL(t *testing.T) {
	root := canonicalTempDir(t)
	if output, err := exec.Command(
		"chmod",
		"+a",
		"everyone deny delete_child,file_inherit,directory_inherit",
		root,
	).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a returned error: %v: %s", err, output)
	}
	defer exec.Command("chmod", "-N", root).Run()
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	var stagePath string
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		stagePath = writer.(*rootedTreeWriterUnix).prepared.stagePath
		return writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("planned"))
	})
	if stagePath != "" {
		defer func() {
			_ = exec.Command("chmod", "-RN", stagePath).Run()
			_ = os.RemoveAll(stagePath)
		}()
	}
	if prepared != nil {
		t.Fatal("PrepareRootedTree returned a stage with inherited ACL metadata")
	}
	assertFailure(t, err, failureUnsupportedGuarantee, phaseCreateTemporary)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("ACL-bearing tree was published: %v", statErr)
	}
}

func TestPrepareRootedTreeRejectsDescendantFileFlag(t *testing.T) {
	root := canonicalTempDir(t)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	var entry string
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("planned")); err != nil {
			return err
		}
		concrete := writer.(*rootedTreeWriterUnix)
		entry = filepath.Join(concrete.prepared.stagePath, "entry")
		return unix.Chflags(entry, unix.UF_NODUMP)
	})
	if entry != "" {
		defer unix.Chflags(entry, 0)
	}
	if prepared != nil {
		t.Fatal("PrepareRootedTree returned a stage with file flags")
	}
	assertFailure(t, err, failureUnsupportedGuarantee, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("flag-bearing tree was published: %v", statErr)
	}
}

func TestPrepareRootedTreeRejectsDarwinProvenanceXattr(t *testing.T) {
	root := canonicalTempDir(t)
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, "published")
	provenanceChanged := false
	prepared, err := PrepareRootedTree(t.Context(), capability, func(writer mutationfs.RootedTreeWriter) error {
		if err := writer.WriteFile(treePathForTest(t, "entry"), 0o600, strings.NewReader("planned")); err != nil {
			return err
		}
		concrete := writer.(*rootedTreeWriterUnix)
		entry := filepath.Join(concrete.prepared.stagePath, "entry")
		if err := unix.Setxattr(entry, "com.apple.provenance", []byte("unrepresented"), 0); err != nil {
			if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
				t.Skipf("extended attributes unavailable: %v", err)
			}
			return err
		}
		size, err := unix.Getxattr(entry, "com.apple.provenance", nil)
		if err != nil {
			return err
		}
		value := make([]byte, size)
		if _, err := unix.Getxattr(entry, "com.apple.provenance", value); err != nil {
			return err
		}
		provenanceChanged = string(value) == "unrepresented"
		return nil
	})
	if !provenanceChanged {
		if prepared == nil {
			t.Fatalf("PrepareRootedTree failed before provenance mutability could be determined: %v", err)
		}
		if abortErr := prepared.Abort(t.Context()); abortErr != nil {
			t.Fatalf("abort prepared tree after immutable provenance probe: %v", abortErr)
		}
		t.Skip("macOS retained its kernel-owned provenance value")
	}
	if prepared != nil {
		t.Fatal("PrepareRootedTree returned a stage with unrepresented provenance metadata")
	}
	assertFailure(t, err, failureUnsupportedGuarantee, phaseValidate)
	assertClosedRootedCapability(t, capability)
	if _, statErr := os.Lstat(filepath.Join(root, "published")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("provenance-bearing tree was published: %v", statErr)
	}
}
