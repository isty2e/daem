package journal

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

type journalPublishFailure struct {
	kind mutationfs.FailureKind
}

func (failure journalPublishFailure) Error() string {
	return "injected journal publication failure"
}

func (failure journalPublishFailure) Kind() mutationfs.FailureKind {
	return failure.kind
}

type journalPublishFailingFilesystem struct {
	mutationfs.Store
	failure error
}

func (filesystem journalPublishFailingFilesystem) PrepareRootedTree(
	ctx context.Context,
	capability rootedpath.CommitCapability,
	populate func(mutationfs.RootedTreeWriter) error,
) (mutationfs.PreparedRootedTree, error) {
	prepared, err := filesystem.Store.PrepareRootedTree(ctx, capability, populate)
	if err != nil {
		return nil, err
	}
	return &journalPublishFailingPreparedTree{
		prepared: prepared,
		failure:  filesystem.failure,
	}, nil
}

type journalPublishFailingPreparedTree struct {
	prepared mutationfs.PreparedRootedTree
	failure  error
}

func (prepared *journalPublishFailingPreparedTree) Commit(ctx context.Context) error {
	kind, _ := mutationfs.FailureKindOf(prepared.failure)
	if kind == mutationfs.FailureIndeterminateCommit {
		if err := prepared.prepared.Commit(ctx); err != nil {
			return err
		}
	} else if err := prepared.prepared.Abort(ctx); err != nil {
		return err
	}
	return prepared.failure
}

func (prepared *journalPublishFailingPreparedTree) Abort(ctx context.Context) error {
	return prepared.prepared.Abort(ctx)
}

func TestCaptureJournalLeavesPrivateResidueUntouchedWhenActiveEvidenceBlocksCapture(
	t *testing.T,
) {
	recoveryRoot := t.TempDir()
	privatePath := filepath.Join(recoveryRoot, ".private-build-residue")
	if err := os.Mkdir(privatePath, 0o700); err != nil {
		t.Fatalf("create private residue: %v", err)
	}
	activePath := filepath.Join(recoveryRoot, "20260720T120000.000000000Z-apply")
	if err := os.Mkdir(activePath, 0o700); err != nil {
		t.Fatalf("create active recovery operation: %v", err)
	}

	_, err := CaptureJournalWithOptions(
		context.Background(),
		Paths{RecoveryDir: recoveryRoot},
		"20260720T130000.000000000Z-apply",
		time.Now().UTC(),
		durable.Snapshot{},
		durable.Snapshot{},
		CaptureOptions{
			Filesystem:   journalTestFilesystem(),
			Resolver:     func(output.Destination) (string, error) { return "", nil },
			StateEncoder: testStateCodec(),
		},
	)
	if err == nil || !strings.Contains(err.Error(), "interrupted apply operation found") {
		t.Fatalf("CaptureJournalWithOptions error = %v, want active recovery rejection", err)
	}
	for _, path := range []string{privatePath, activePath} {
		if _, statErr := os.Lstat(path); statErr != nil {
			t.Fatalf("blocked capture changed recovery evidence %q: %v", path, statErr)
		}
	}
}

func TestCaptureJournalPublicationFailureFollowsVisibilityClassification(t *testing.T) {
	for _, test := range []struct {
		name        string
		kind        mutationfs.FailureKind
		wantVisible bool
	}{
		{name: "uncommitted", kind: mutationfs.FailureUncommitted},
		{
			name:        "indeterminate",
			kind:        mutationfs.FailureIndeterminateCommit,
			wantVisible: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			recoveryRoot := filepath.Join(t.TempDir(), "recovery")
			operationID := "20260720T140000.000000000Z-apply"
			filesystem := journalPublishFailingFilesystem{
				Store:   journalTestFilesystem(),
				failure: journalPublishFailure{kind: test.kind},
			}

			_, err := CaptureJournalWithOptions(
				t.Context(),
				Paths{RecoveryDir: recoveryRoot},
				operationID,
				time.Now().UTC(),
				beforeStatefile(),
				afterStatefile(),
				CaptureOptions{
					Filesystem:   filesystem,
					Resolver:     func(output.Destination) (string, error) { return "", nil },
					StateEncoder: testStateCodec(),
				},
			)
			if err == nil || !strings.Contains(err.Error(), "commit recovery journal") {
				t.Fatalf("CaptureJournalWithOptions error = %v, want publication failure", err)
			}

			_, statErr := os.Stat(filepath.Join(recoveryRoot, operationID))
			visible := statErr == nil
			if statErr != nil && !os.IsNotExist(statErr) {
				t.Fatalf("stat recovery operation: %v", statErr)
			}
			if visible != test.wantVisible {
				t.Fatalf(
					"recovery operation visible = %t, want %t after %s failure",
					visible,
					test.wantVisible,
					test.kind,
				)
			}
		})
	}
}

func TestCaptureJournalRejectsMismatchedOperationAuthorityBeforeConstruction(t *testing.T) {
	selectedRoot := t.TempDir()
	recoveryRoot := filepath.Join(selectedRoot, "recovery")
	capturedRoot, err := rootedpath.CaptureRoot(selectedRoot)
	if err != nil {
		t.Fatalf("capture selected root: %v", err)
	}
	t.Cleanup(func() { _ = capturedRoot.Close() })
	wrongPath := filepath.Join(recoveryRoot, "wrong-operation")
	authority, err := rootedpath.BindSelectedEntryAuthority(
		capturedRoot,
		selectedRoot,
		wrongPath,
	)
	if err != nil {
		t.Fatalf("bind wrong operation authority: %v", err)
	}
	t.Cleanup(func() { _ = authority.Close() })

	_, err = CaptureJournalWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot},
		"20260720T150000.000000000Z-apply",
		time.Now().UTC(),
		beforeStatefile(),
		afterStatefile(),
		CaptureOptions{
			Filesystem:         journalTestFilesystem(),
			Resolver:           func(output.Destination) (string, error) { return "", nil },
			StateEncoder:       testStateCodec(),
			OperationAuthority: authority,
		},
	)
	if err == nil || !strings.Contains(err.Error(), "does not match operation directory") {
		t.Fatalf("CaptureJournalWithOptions error = %v, want authority mismatch", err)
	}
	for _, path := range []string{
		wrongPath,
		filepath.Join(recoveryRoot, "20260720T150000.000000000Z-apply"),
	} {
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			t.Fatalf("mismatched authority created %q or stat failed: %v", path, statErr)
		}
	}
}

func TestRemovePrivateBuildTreeHandlesReadOnlyDirectoriesWithoutFollowingSymlinks(
	t *testing.T,
) {
	t.Run("read-only directory", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "build")
		nested := filepath.Join(root, "files", "000001")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatalf("create private build tree: %v", err)
		}
		if err := os.WriteFile(filepath.Join(nested, "entry"), []byte("private"), 0o400); err != nil {
			t.Fatalf("write private build entry: %v", err)
		}
		if err := os.Chmod(nested, 0o500); err != nil {
			t.Fatalf("make private build directory read-only: %v", err)
		}

		if err := removePrivateBuildTree(root); err != nil {
			t.Fatalf("removePrivateBuildTree: %v", err)
		}
		if _, err := os.Lstat(root); !os.IsNotExist(err) {
			t.Fatalf("private build tree stat after cleanup = %v, want absence", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "build")
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatalf("create private build root: %v", err)
		}
		external := t.TempDir()
		canary := filepath.Join(external, "canary")
		if err := os.WriteFile(canary, []byte("external"), 0o600); err != nil {
			t.Fatalf("write external canary: %v", err)
		}
		if err := os.Symlink(external, filepath.Join(root, "external")); err != nil {
			t.Fatalf("create private build symlink: %v", err)
		}

		err := removePrivateBuildTree(root)
		if err == nil || !strings.Contains(err.Error(), "contains symlink") {
			t.Fatalf("removePrivateBuildTree error = %v, want symlink refusal", err)
		}
		content, readErr := os.ReadFile(canary)
		if readErr != nil || string(content) != "external" {
			t.Fatalf("external canary changed: content=%q err=%v", content, readErr)
		}
	})
}
