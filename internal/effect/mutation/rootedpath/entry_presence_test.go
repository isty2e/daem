//go:build darwin || linux || windows

package rootedpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCapturedRootSingleChildPresenceUsesTheExistingNoFollowContract(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "present"), []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "directory"), 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := CaptureRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	validation, err := capturedRootValidationPathComponents(&root.platform)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name    string
		present bool
	}{{"present", true}, {"directory", true}, {"missing", false}} {
		t.Run(row.name, func(t *testing.T) {
			budget := &referentTestBudget{limit: validation + 1}
			present, err := root.ChildExistsNoFollow(t.Context(), row.name, budget)
			if err != nil || present != row.present || budget.visits != budget.limit {
				t.Fatal("single presence observation changed existence or its charged work", present, err, budget.visits)
			}
			if present, err := root.ChildExistsNoFollow(t.Context(), row.name, budget); !errors.Is(err, errReferentTestBudget) || present {
				t.Fatal("an exhausted probe became known presence/absence", present, err)
			}
		})
	}
	t.Run("dangling is an entry", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(base, "missing-target"), filepath.Join(base, "dangling")); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		present, err := root.ChildExistsNoFollow(t.Context(), "dangling", &referentTestBudget{limit: validation + 1})
		if err != nil || !present {
			t.Fatal("the no-follow presence query followed a dangling target", present, err)
		}
	})
	if present, err := root.ChildExistsNoFollow(t.Context(), "../outside", &referentTestBudget{limit: 1024}); !hasFailureKind(err, FailureInvalidDestination) || present {
		t.Fatal("a child name became a path", present, err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if present, err := root.ChildExistsNoFollow(t.Context(), "present", &referentTestBudget{limit: 1024}); !hasFailureKind(err, FailureRootUnavailable) || present {
		t.Fatal("a closed root supplied child presence", present, err)
	}
}
