//go:build linux

package commit

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReplacementRefusesBeforeWritingPayload(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writeTestFile(t, path, "before", 0o640)
	expected := captureIdentity(t, path)
	request, err := NewFileReplacement(path, []byte("after"), 0o640, expected)
	if err != nil {
		t.Fatal(err)
	}
	denied := &SELinuxLabelError{Destination: path, Err: unsupported("label not permitted", unix.EACCES)}
	faults := faultPlan{
		afterEffectFailures: map[phase]error{phasePrepareFileAccess: denied},
		actions:             map[phase]func(){phaseWritePayload: func() { t.Fatal("wrote before confirming the access context") }},
	}
	err = commitFileWithFaults(t.Context(), request, faults)
	assertFailure(t, err, failureUnsupportedGuarantee, phasePrepareFileAccess)
	if !errors.Is(err, denied) || !expected.Equal(captureIdentity(t, path)) {
		t.Fatal("lost the refusal or changed the original", err)
	}
	assertFile(t, path, "before", 0o640)
	assertNoPrivateEntries(t, root)
}

func TestSELinuxReplacementLabel(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	writeTestFile(t, path, "before", 0o640)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	label, err := readSELinuxLabel(int(file.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	if label == nil {
		t.Skip("requires a filesystem with SELinux labels")
	}
	parts := bytes.SplitN(label, []byte(":"), 4)
	if len(parts) < 3 {
		t.Fatalf("invalid SELinux context: %q", label)
	}
	parts[2] = []byte("cache_home_t")
	other := bytes.Join(parts, []byte(":"))
	if bytes.Equal(other, label) {
		t.Skip("fixture already has the cache file type")
	}
	if err := unix.Fsetxattr(int(file.Fd()), linuxSELinuxLabel, other, 0); err != nil {
		if errors.Is(err, fs.ErrPermission) || errors.Is(err, unix.EINVAL) {
			t.Skipf("policy does not permit the alternate test context: %v", err)
		}
		t.Fatal(err)
	}
	original, err := readSELinuxLabel(int(file.Fd()))
	if err != nil || original == nil || bytes.Equal(original, label) {
		t.Fatalf("could not establish an alternate context: %q, %v", original, err)
	}

	request, err := NewFileReplacement(path, []byte("after"), 0o640, captureIdentity(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if err := CommitFile(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	updated, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer updated.Close()
	if actual, err := readSELinuxLabel(int(updated.Fd())); err != nil || !bytes.Equal(actual, original) {
		t.Fatalf("updated label = %q, want %q: %v", actual, original, err)
	}
	assertFile(t, path, "after", 0o640)
}

func TestUnreadableFilePublicationRevalidatesMetadataNames(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged owner to exercise metadata access")
	}
	for _, test := range []struct {
		name   string
		mode   fs.FileMode
		create bool
		add    bool
	}{
		{name: "write_only_removed", mode: 0o200},
		{name: "write_only_added", mode: 0o200, add: true},
		{name: "no_access_removed", mode: 0o000},
		{name: "create_extra_name_allowed", mode: 0o200, create: true, add: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
			request, err := NewFileCreate(path, []byte("after"), test.mode)
			if !test.create {
				writeTestFile(t, path, "before", 0o600)
				if err := unix.Setxattr(path, "user.daem.test", []byte("preserved"), 0); err != nil {
					t.Fatal(err)
				}
				identity, captureErr := CaptureEntryIdentity(t.Context(), path)
				if captureErr != nil {
					t.Fatal(captureErr)
				}
				request, err = NewFileReplacement(path, []byte("after"), test.mode, identity)
			}
			if err != nil {
				t.Fatal(err)
			}
			var mutationErr error
			faults := faultPlan{actions: map[phase]func(){phaseSyncParent: func() {
				if test.mode == 0 {
					if mutationErr = os.Chmod(path, 0o600); mutationErr != nil {
						return
					}
				}
				if test.add {
					mutationErr = unix.Setxattr(path, "user.daem.extra", []byte("added"), 0)
				} else {
					mutationErr = unix.Removexattr(path, "user.daem.test")
				}
				if test.mode == 0 {
					if chmodErr := os.Chmod(path, test.mode); mutationErr == nil {
						mutationErr = chmodErr
					}
				}
			}}}
			err = commitFileWithFaults(t.Context(), request, faults)
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			if test.add {
				if err != nil {
					t.Fatal("publication rejected an unrelated new attribute", err)
				}
			} else {
				assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
			}
			if info, statErr := os.Stat(path); statErr != nil || info.Mode().Perm() != test.mode {
				t.Fatal("metadata verification changed the published mode", statErr)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			assertFile(t, path, "after", 0o600)
			assertNoPrivateEntries(t, root)
		})
	}
}
