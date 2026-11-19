package lock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	locksnapshot "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
)

func TestLockRebuildRespectsLinkedDestinationAndConsumedSources(t *testing.T) {
	damaged := fmt.Sprintf("version = %d\n[[\n", locksnapshot.CurrentVersion)
	for _, test := range []struct {
		name        string
		referent    string
		declaration string
		wantOverlap bool
		missing     bool
	}{
		{"separate referent", "other/lock.toml", "[instructions.project]\nsource = 'source.md'\n", false, false},
		{"source file", "source.md", "[instructions.project]\nsource = 'source.md'\n", true, false},
		{"inside directory source", "probe/lock.toml", "[[skill]]\nname = 'probe'\nsource = { path = 'probe', mode = 'vendor' }\n", true, false},
		{"dangling", "missing.toml", "[instructions.project]\nsource = 'source.md'\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
			manifest := filepath.Join(root, "daem.toml")
			selected := filepath.Join(root, "daem.lock.toml")
			target := filepath.Join(root, test.referent)
			writeWorkflowTestFile(t, root, "daem.toml", "version = 1\ntargets = ['codex']\n"+test.declaration)
			writeWorkflowTestFile(t, root, "source.md", "project instructions\n")
			writeWorkflowTestFile(t, root, "probe/SKILL.md", "---\nname: probe\ndescription: fixture\n---\n")
			if !test.missing {
				writeWorkflowTestFile(t, root, test.referent, damaged)
			}
			if err := os.Symlink(test.referent, selected); err != nil {
				t.Fatal(err)
			}

			for _, dryRun := range []bool{true, false} {
				sourceEvents := 0
				result, err := RunLock(t.Context(), LockInput{
					ManifestPath: manifest,
					DryRun:       dryRun,
					SourceEvents: func(acquisition.Event) { sourceEvents++ },
				})
				switch {
				case test.wantOverlap:
					if err == nil || !strings.Contains(err.Error(), "destination overlaps") || sourceEvents != 0 {
						t.Fatalf("dry-run=%t overlap: %v; source events = %d", dryRun, err, sourceEvents)
					}
				case test.missing:
					if err == nil || sourceEvents != 0 {
						t.Fatalf("dry-run=%t dangling: %v; source events = %d", dryRun, err, sourceEvents)
					}
				default:
					if err != nil || result.PreviousStatus() != PreviousDamaged {
						t.Fatalf("dry-run=%t on independent referent: %q, %v", dryRun, result.PreviousStatus(), err)
					}
				}
				if link, err := os.Readlink(selected); err != nil || link != test.referent {
					t.Fatalf("selected link changed: %q, %v", link, err)
				}
				if test.missing {
					if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("dangling referent was created: %v", err)
					}
				} else if dryRun || test.wantOverlap {
					if content, err := os.ReadFile(target); err != nil || string(content) != damaged {
						t.Fatalf("referent changed: %q, %v", content, err)
					}
				} else if _, err := lockfile.Load(t.Context(), target); err != nil {
					t.Fatalf("regenerated referent is unreadable: %v", err)
				}
			}
		})
	}
}

func TestLockRebuildRefusesRetargetedLinkBeforeWriting(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	selected := filepath.Join(root, "daem.lock.toml")
	damaged := fmt.Sprintf("version = %d\n[[\n", locksnapshot.CurrentVersion)
	future := fmt.Sprintf("version = %d\n", locksnapshot.CurrentVersion+1)
	writeWorkflowTestFile(t, root, "damaged.toml", damaged)
	writeWorkflowTestFile(t, root, "future.toml", future)
	writeWorkflowTestFile(t, root, "daem.toml", "version = 1\ntargets = ['codex']\n")
	if err := os.Symlink("damaged.toml", selected); err != nil {
		t.Fatal(err)
	}

	_, err := RunLock(t.Context(), LockInput{
		ManifestPath: filepath.Join(root, "daem.toml"),
		PrepareResult: func(result Result) error {
			if result.PreviousStatus() != PreviousDamaged {
				t.Fatal("preparation did not reach the damaged lock rebuild")
			}
			intervening := filepath.Join(root, "next-link")
			if err := os.Symlink("future.toml", intervening); err != nil {
				t.Fatal(err)
			}
			return os.Rename(intervening, selected)
		},
	})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatalf("retargeted lock was not refused as stale: %v", err)
	}
	for name, want := range map[string]string{"damaged.toml": damaged, "future.toml": future} {
		if content, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(content) != want {
			t.Fatalf("%s = %q, %v", name, content, err)
		}
	}
	if link, err := os.Readlink(selected); err != nil || link != "future.toml" {
		t.Fatalf("intervening link changed: %q, %v", link, err)
	}
}
