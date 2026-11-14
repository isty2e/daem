//go:build darwin || linux

package fileset

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

func TestPublicationCleanupSelectsOnlyOwnedFlatArtifacts(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(root, fileSetTemporaryPrefix+strings.Repeat("a", 32))
	retired := filepath.Join(root, publicationRetiredPrefix+strings.Repeat("b", 32))
	unknown := filepath.Join(root, publicationRetiredPrefix+strings.Repeat("c", 32))
	link := filepath.Join(root, fileSetTemporaryPrefix+strings.Repeat("d", 32))
	for _, path := range []string{stage, retired, unknown} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, filepath.Join(retired, publicationIntentFile), "{torn", 0o600)
	writeFixture(t, filepath.Join(retired, publicationBackupName(1000)), "private", 0o600)
	writeFixture(t, filepath.Join(unknown, "user-file"), "preserve", 0o600)
	canary := filepath.Join(t.TempDir(), "canary")
	writeFixture(t, canary, "outside", 0o600)
	if err := os.Symlink(filepath.Dir(canary), link); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectPublicationCleanup(t.Context(), root, &fenceObservationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if len(plan.Paths()) != 2 || len(plan.Deferred()) != 2 {
		t.Fatal("cleanup lost its selected/deferred partition", plan.Paths(), plan.Deferred())
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatal("preview wrote", err)
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, stage)
	assertMissing(t, retired)
	for _, path := range []string{filepath.Join(unknown, "user-file"), link, canary} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatal("collection extended outside its admitted artifacts", path, err)
		}
	}
}

func TestRetiredCollectionCannotInheritAReplacementRoot(t *testing.T) {
	root := mustCanonicalStateDir(t, filepath.Join(t.TempDir(), "metadata"))
	name := publicationRetiredPrefix + strings.Repeat("e", 32)
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	identity, err := storagecommit.CaptureEntryIdentity(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(root, root+".saved"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(path, publicationIntentFile), "replacement", 0o600)
	collectRetiredPublication(t.Context(), root, name, identity)
	content, err := os.ReadFile(filepath.Join(path, publicationIntentFile))
	if err != nil || string(content) != "replacement" {
		t.Fatal("replacement namespace inherited retired-object cleanup authority", err)
	}
}

func TestPublicationCleanupRevalidatesBeforeEffects(t *testing.T) {
	for _, change := range []string{"replace", "new child", "active", "cancel"} {
		t.Run(change, func(t *testing.T) {
			root := mustCanonicalStateDir(t, t.TempDir())
			if err := os.Chmod(root, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, fileSetTemporaryPrefix+strings.Repeat("a", 32))
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			plan, err := InspectPublicationCleanup(t.Context(), root, &fenceObservationBudget{})
			if err != nil || len(plan.Paths()) != 1 {
				t.Fatal(plan, err)
			}
			defer plan.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch change {
			case "replace":
				if err := os.Rename(path, path+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "new child":
				writeFixture(t, filepath.Join(path, "user-file"), "preserve", 0o600)
			case "active":
				if err := os.Mkdir(transactionDir(root), 0o700); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			}
			if err := plan.Execute(ctx); err == nil {
				t.Fatal("stale/canceled cleanup succeeded")
			}
			if _, err := os.Stat(path); err != nil {
				t.Fatal("refused cleanup changed its candidate", err)
			}
		})
	}
}

func TestPublicationCleanupReservesItsDisclosedBatch(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for index := range 30 {
		path := filepath.Join(root, fmt.Sprintf("%s%032x", fileSetTemporaryPrefix, index))
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := InspectPublicationCleanup(t.Context(), root, &fenceObservationBudget{})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if len(plan.Paths()) != 30 {
		t.Fatal("small artifacts were unnecessarily deferred", len(plan.Paths()), plan.Deferred())
	}
	if err := plan.Execute(t.Context()); err != nil {
		t.Fatal("disclosed cleanup could not execute its admitted work", err)
	}
	for _, path := range plan.Paths() {
		assertMissing(t, path)
	}
	if err := plan.Execute(t.Context()); err == nil {
		t.Fatal("cleanup reused consumed execution admission")
	}
}

func TestPublicationCleanupMakesBoundedProgressAcrossBatches(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	const count = 60
	for index := range count {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("%s%032x", fileSetTemporaryPrefix, index)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	remaining := count
	for remaining != 0 {
		plan, err := InspectPublicationCleanup(t.Context(), root, &fenceObservationBudget{})
		if err != nil {
			t.Fatal(err)
		}
		selected := len(plan.Paths())
		if selected == 0 || selected > remaining {
			plan.Close()
			t.Fatal("bounded cleanup cannot make progress", selected, remaining)
		}
		if err := plan.Execute(t.Context()); err != nil {
			plan.Close()
			t.Fatal("selected batch exceeded its reservation", err)
		}
		plan.Close()
		remaining -= selected
	}
}

func TestPublicationEmptyRemovalPreservesModeDrift(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	name := fileSetTemporaryPrefix + strings.Repeat("f", 32)
	path := filepath.Join(root, name)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := InspectPublicationCleanup(t.Context(), root, &fenceObservationBudget{})
	if err != nil || len(plan.selected) != 1 {
		t.Fatal(plan, err)
	}
	defer plan.Close()
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := plan.remove(t.Context(), name, plan.selected[0].snapshot.RootIdentity(), 0, &fenceObservationBudget{}); err == nil {
		t.Fatal("same-object refresh ignored protection drift")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatal("mode-drift refusal removed or repaired a foreign modification", err)
	}
}

func TestPublicationCleanupBudgetDefersWithoutCreatingAFence(t *testing.T) {
	root := mustCanonicalStateDir(t, t.TempDir())
	if err := os.Mkdir(filepath.Join(root, fileSetTemporaryPrefix+strings.Repeat("a", 32)), 0o700); err != nil {
		t.Fatal(err)
	}
	denied := errors.New("no optional collection work")
	budget := &publicationReadAdmissionBudget{onWork: func(entries int, _ int64) error {
		if entries > 1 {
			return denied
		}
		return nil
	}}
	plan, err := InspectPublicationCleanup(t.Context(), root, budget)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if len(plan.Paths()) != 0 || len(plan.Deferred()) == 0 {
		t.Fatal("exhausted GC looked complete", plan.Paths(), plan.Deferred())
	}
	if err := ObserveClearPublicationFenceAt(t.Context(), root, 256, &fenceObservationBudget{}); err != nil {
		t.Fatal("optional budget became a global fence", err)
	}
}
