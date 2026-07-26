//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func TestRootedFileCommitRejectsAncestorAliasesWithZeroReferentWrites(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, root string, outside string)
		want       rootedpath.FailureKind
		unsafePath func(root string, outside string) string
	}{
		{
			name: "outside symlink",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.Symlink(outside, filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create outside ancestor symlink: %v", err)
				}
			},
			want: rootedpath.FailureAncestorSymlink,
			unsafePath: func(_ string, outside string) string {
				return filepath.Join(outside, "skills")
			},
		},
		{
			name: "inside symlink",
			prepare: func(t *testing.T, root string, _ string) {
				t.Helper()
				inside := filepath.Join(root, "managed")
				if err := os.Mkdir(inside, 0o700); err != nil {
					t.Fatalf("create inside referent: %v", err)
				}
				if err := os.Symlink(inside, filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create inside ancestor symlink: %v", err)
				}
			},
			want: rootedpath.FailureAncestorSymlink,
			unsafePath: func(root string, _ string) string {
				return filepath.Join(root, "managed", "skills")
			},
		},
		{
			name: "dangling symlink",
			prepare: func(t *testing.T, root string, outside string) {
				t.Helper()
				if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, ".agents")); err != nil {
					t.Fatalf("create dangling ancestor symlink: %v", err)
				}
			},
			want: rootedpath.FailureDanglingAncestorSymlink,
			unsafePath: func(_ string, outside string) string {
				return filepath.Join(outside, "missing")
			},
		},
		{
			name: "non directory",
			prepare: func(t *testing.T, root string, _ string) {
				t.Helper()
				writeTestFile(t, filepath.Join(root, ".agents"), "not a directory", 0o600)
			},
			want: rootedpath.FailureAncestorNotDirectory,
			unsafePath: func(root string, _ string) string {
				return filepath.Join(root, ".agents", "skills")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := t.TempDir()
			root := filepath.Join(parent, "project")
			outside := filepath.Join(parent, "outside")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatalf("create captured root: %v", err)
			}
			if err := os.Mkdir(outside, 0o700); err != nil {
				t.Fatalf("create outside root: %v", err)
			}
			captured := captureRootForCommitTest(t, root)
			test.prepare(t, root, outside)

			capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review/SKILL.md")
			request, err := NewRootedFileCreate(capability, []byte("unsafe"), 0o600)
			if err != nil {
				t.Fatalf("NewRootedFileCreate returned error: %v", err)
			}
			err = CommitFile(context.Background(), request)
			if !hasRootedPathFailureKind(err, test.want) {
				t.Fatalf("CommitFile error = %v, want project failure %s", err, test.want)
			}
			assertClosedRootedCapability(t, capability)
			if _, statErr := os.Lstat(test.unsafePath(root, outside)); !os.IsNotExist(statErr) && !errors.Is(statErr, syscall.ENOTDIR) {
				t.Fatalf("unsafe referent path was touched: %v", statErr)
			}
			if test.name == "non directory" {
				assertFileContent(t, filepath.Join(root, ".agents"), "not a directory")
			}
		})
	}
}

func TestRootedFileCommitRejectsFinalSymlinkWithoutChangingReferent(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.MkdirAll(filepath.Join(root, ".agents"), 0o700); err != nil {
		t.Fatalf("create project ancestry: %v", err)
	}
	referent := filepath.Join(parent, "outside")
	writeTestFile(t, referent, "keep", 0o600)
	destination := filepath.Join(root, ".agents", "config")
	if err := os.Symlink(referent, destination); err != nil {
		t.Fatalf("create final symlink: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	request, err := NewRootedFileCreate(capability, []byte("replace"), 0o600)
	if err != nil {
		t.Fatalf("NewRootedFileCreate returned error: %v", err)
	}
	err = CommitFile(context.Background(), request)
	if !hasRootedPathFailureKind(err, rootedpath.FailureFinalSymlink) {
		t.Fatalf("CommitFile error = %v, want %s", err, rootedpath.FailureFinalSymlink)
	}
	assertClosedRootedCapability(t, capability)
	assertFileContent(t, referent, "keep")

	replacementCapability := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	replacementPath, err := replacementCapability.Destination().LexicalPath()
	if err != nil {
		t.Fatalf("project replacement lexical path: %v", err)
	}
	expected, err := CaptureEntryIdentity(context.Background(), replacementPath)
	if err != nil {
		t.Fatalf("CaptureEntryIdentity returned error: %v", err)
	}
	_, err = NewRootedFileReplacement(replacementCapability, []byte("replace"), 0o600, expected)
	if !hasRootedPathFailureKind(err, rootedpath.FailureFinalSymlink) {
		t.Fatalf("NewRootedFileReplacement error = %v, want %s", err, rootedpath.FailureFinalSymlink)
	}
	if closeErr := replacementCapability.Close(); closeErr != nil {
		t.Fatalf("close rejected replacement capability: %v", closeErr)
	}
	assertFileContent(t, referent, "keep")
}

func TestRootedFileCommitRejectsRootReplacementBeforeEffect(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create captured root: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/config")
	request, err := NewRootedFileCreate(capability, []byte("blocked"), 0o600)
	if err != nil {
		t.Fatalf("NewRootedFileCreate returned error: %v", err)
	}
	moved := filepath.Join(parent, "moved-project")
	if err := os.Rename(root, moved); err != nil {
		t.Fatalf("move captured root: %v", err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatalf("create replacement root: %v", err)
	}
	err = CommitFile(context.Background(), request)
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) {
		t.Fatalf("CommitFile error = %v, want %s", err, rootedpath.FailureRootReplaced)
	}
	assertClosedRootedCapability(t, capability)
	for _, path := range []string{filepath.Join(root, ".agents"), filepath.Join(moved, ".agents")} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("root replacement produced an effect at %q: %v", path, statErr)
		}
	}
}

func TestRootedFileCommitReportsIndeterminateWhenAncestorMovesAtVisibility(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(filepath.Join(root, ".agents", "skills"), 0o700); err != nil {
		t.Fatalf("create project ancestry: %v", err)
	}
	if err := os.Mkdir(outside, 0o700); err != nil {
		t.Fatalf("create outside directory: %v", err)
	}
	captured := captureRootForCommitTest(t, root)
	capability := rootedCapabilityForCommitTest(t, captured, ".agents/skills/review.md")
	request, err := NewRootedFileCreate(capability, []byte("indeterminate"), 0o600)
	if err != nil {
		t.Fatalf("NewRootedFileCreate returned error: %v", err)
	}
	movedAgents := filepath.Join(outside, "moved-agents")
	faults := faultPlan{actions: map[phase]func(){
		phaseCommitEntry: func() {
			if err := os.Rename(filepath.Join(root, ".agents"), movedAgents); err != nil {
				t.Fatalf("move ancestor at visibility: %v", err)
			}
			if err := os.Mkdir(filepath.Join(root, ".agents"), 0o700); err != nil {
				t.Fatalf("replace ancestor at visibility: %v", err)
			}
		},
	}}
	err = commitFileWithFaults(context.Background(), request, faults)
	assertFailure(t, err, failureIndeterminateCommit, phaseVerifyEntry)
	if !hasRootedPathFailureKind(err, rootedpath.FailureAncestorChanged) {
		t.Fatalf("CommitFile error = %v, want nested %s", err, rootedpath.FailureAncestorChanged)
	}
	assertClosedRootedCapability(t, capability)
	assertFileContent(t, filepath.Join(movedAgents, "skills", "review.md"), "indeterminate")
	if _, statErr := os.Lstat(filepath.Join(root, ".agents", "skills")); !os.IsNotExist(statErr) {
		t.Fatalf("replacement ancestor received an effect: %v", statErr)
	}
}
