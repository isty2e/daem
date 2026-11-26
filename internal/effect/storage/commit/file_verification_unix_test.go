//go:build darwin || linux

package commit

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestFilePublicationRechecksChangedStageBeforeReplacement(t *testing.T) {
	for _, change := range []string{"tag", "privileged mode", "content"} {
		t.Run(change, func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			writeTestFile(t, path, "before", 0o600)
			request, err := NewFileReplacement(path, []byte("after"), 0o600, captureIdentity(t, path))
			if err != nil {
				t.Fatal(err)
			}
			faults := faultPlan{actions: map[phase]func(){phaseCommitEntry: func() {
				entries, err := os.ReadDir(root)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), temporaryPrefix) {
						continue
					}
					stage := filepath.Join(root, entry.Name())
					switch change {
					case "tag":
						err = unix.Setxattr(stage, "user.daem.note", []byte("tag"), 0)
					case "privileged mode":
						err = unix.Chmod(stage, 0o2600)
					case "content":
						err = os.WriteFile(stage, []byte("other"), 0o600)
					}
					if err != nil {
						t.Fatal(err)
					}
					return
				}
				t.Fatal("staged file not found")
			}}}
			err = commitFileWithFaults(t.Context(), request, faults)
			if change == "tag" {
				if err != nil {
					t.Fatal(err)
				}
				assertFile(t, path, "after", 0o600)
			} else {
				if err == nil {
					t.Fatal("publication accepted the changed stage")
				}
				assertFile(t, path, "before", 0o600)
			}
		})
	}
}

func TestCommitFileVerifiesSameInodePayloadAfterVisibility(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	request, err := NewFileCreate(path, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var mutationErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseVerifyEntry: func() { mutationErr = os.WriteFile(path, []byte("changed"), 0o600) },
	}}
	err = commitFileWithFaults(context.Background(), request, faults)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertFile(t, path, "changed", 0o600)
	assertNoPrivateEntries(t, root)
}

func TestCommitFileVerifiesPayloadAfterNamespaceSync(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	request, err := NewFileCreate(path, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var mutationErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseSyncParent: func() { mutationErr = os.WriteFile(path, []byte("changed"), 0o600) },
	}}
	err = commitFileWithFaults(t.Context(), request, faults)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertFile(t, path, "changed", 0o600)
	assertNoPrivateEntries(t, root)
}

func TestCommitFileVerifiesFinalMode(t *testing.T) {
	root := canonicalTempDir(t)
	path := filepath.Join(root, "config")
	request, err := NewFileCreate(path, []byte("payload"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var mutationErr error
	faults := faultPlan{actions: map[phase]func(){
		phaseVerifyEntry: func() { mutationErr = os.Chmod(path, 0o644) },
	}}
	err = commitFileWithFaults(t.Context(), request, faults)
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	assertFile(t, path, "payload", 0o644)
	assertNoPrivateEntries(t, root)
}

func TestCommitFilePublishesBeforeRestrictiveMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged owner to exercise mode-dependent metadata access")
	}
	const name = "user.daem.test"
	for _, mode := range []fs.FileMode{0o000, 0o200, 0o400, 0o644} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			root := canonicalTempDir(t)
			path := filepath.Join(root, "config")
			writeTestFile(t, path, "before", 0o600)
			t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
			attribute := []byte("preserved")
			if runtime.GOOS == "linux" {
				if err := unix.Setxattr(path, name, attribute, 0); err != nil {
					t.Fatal(err)
				}
			}
			identity, err := CaptureEntryIdentity(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			request, err := NewFileReplacement(path, []byte("after"), mode, identity)
			if err != nil {
				t.Fatal(err)
			}
			if err := CommitFile(t.Context(), request); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Stat(path); err != nil || info.Mode().Perm() != mode {
				t.Fatalf("final mode differs: %v", err)
			}
			if err := os.Chmod(path, 0o600); err != nil {
				t.Fatal(err)
			}
			assertFile(t, path, "after", 0o600)
			if runtime.GOOS == "linux" {
				actual := make([]byte, len(attribute)+1)
				count, err := unix.Getxattr(path, name, actual)
				if err != nil || !bytes.Equal(actual[:count], attribute) {
					t.Fatalf("supported metadata was not preserved: %v", err)
				}
			}
			assertNoPrivateEntries(t, root)
		})
	}
}
