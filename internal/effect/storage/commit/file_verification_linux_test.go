//go:build linux

package commit

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

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
			if test.create {
				if err != nil {
					t.Fatal("creation rejected permitted additional metadata", err)
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
