package declarationartifact

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestRecoveryTargetsRetainObservedLinkContext(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "document")
	if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink("document", alias); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(root, "parent")
	if err := os.Symlink(root, parent); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		paths  []string
		linked bool
	}{
		{"regular", []string{file}, false},
		{"missing", []string{filepath.Join(root, "absent", "lock")}, false},
		{"file link", []string{alias}, true},
		{"parent link", []string{filepath.Join(parent, "document")}, true},
		{"missing through parent", []string{filepath.Join(parent, "absent", "lock")}, true},
		{"mixed pair", []string{file, alias}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets, err := CaptureRecoveryTargets(t.Context(), tc.paths...)
			if err != nil {
				t.Fatal(err)
			}
			cause := fileset.RecoveryAuthorityError{Path: "recorded target"}
			refusal := targets.WrapError(fmt.Errorf("recover: %w", cause))
			var linked LinkedRecoveryError
			if errors.As(refusal, &linked) != tc.linked {
				t.Fatalf("refusal=%v linked=%v, want linked=%v", refusal, linked, tc.linked)
			}
			var authority fileset.RecoveryAuthorityError
			if !errors.As(refusal, &authority) || authority.Path != cause.Path {
				t.Fatalf("lost original authority cause: %v", refusal)
			}
			stale := mutation.StaleSnapshotError{}
			if targets.WrapError(stale) != stale || targets.WrapError(nil) != nil {
				t.Fatal("unrelated failure or success was reclassified")
			}
		})
	}
}

func TestRecoveryTargetsDoNotTreatCaseNormalizationAsLinks(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "document")
	if err := os.WriteFile(file, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	selected := filepath.Join(root, "DOCUMENT")
	if _, err := os.Stat(selected); errors.Is(err, os.ErrNotExist) {
		t.Skip("filesystem uses case-sensitive names")
	} else if err != nil {
		t.Fatal(err)
	}
	targets, err := CaptureRecoveryTargets(t.Context(), selected)
	if err != nil {
		t.Fatal(err)
	}
	var linked LinkedRecoveryError
	if errors.As(targets.WrapError(fileset.RecoveryAuthorityError{Path: "recorded target"}), &linked) {
		t.Fatal("case spelling alone produced link-specific recovery context")
	}
}
