//go:build darwin || linux

package fileset

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestFinalizeCommittedPairPreservesSettledTargets(t *testing.T) {
	for _, test := range []struct {
		name   string
		retain bool
		create bool
	}{
		{name: "replace both"},
		{name: "retain companion", retain: true},
		{name: "create companion", create: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir, manifest, lockfile, marker := setupFinalizationPair(t, test.retain, test.create)
			publishFinalizationImages(t, marker, 2)
			manifestInfo := statFinalizationPath(t, manifest)
			lockInfo := statFinalizationPath(t, lockfile)

			retired, err := FinalizeCommittedPair(t.Context(), stateDir, manifest, lockfile, func(ctx context.Context) error {
				return ctx.Err()
			})
			if err != nil || !retired {
				t.Fatalf("retired = %t; error = %v", retired, err)
			}
			assertMissing(t, transactionDir(stateDir))
			retired, err = FinalizeCommittedPair(t.Context(), stateDir, manifest, lockfile, func(context.Context) error {
				t.Fatal("already clear record entered removal")
				return nil
			})
			if err != nil || retired {
				t.Fatalf("already clear: retired = %t; error = %v", retired, err)
			}
			assertContent(t, manifest, "manifest after")
			wantLock := "lock after"
			if test.retain {
				wantLock = "lock before"
			}
			assertContent(t, lockfile, wantLock)
			for path, info := range map[string]os.FileInfo{manifest: manifestInfo, lockfile: lockInfo} {
				current := statFinalizationPath(t, path)
				if !os.SameFile(info, current) || info.Mode() != current.Mode() {
					t.Fatalf("retirement replaced or changed the mode of %s", path)
				}
			}
		})
	}
}

func TestFinalizeCommittedPairPreservesUnqualifiedEvidence(t *testing.T) {
	for _, test := range []struct {
		name       string
		publish    int
		want       error
		alter      func(t *testing.T, stateDir, target string)
		revalidate bool
	}{
		{name: "mixed", publish: 1, want: ErrUnfinishedFinalizationPair},
		{name: "third image", publish: 2, want: ErrInterruptedFileSetTransaction, alter: func(t *testing.T, _, target string) {
			writeFixture(t, target, "not recorded", 0o640)
		}},
		{name: "extra evidence", publish: 2, want: ErrFileSetEvidenceInvalid, alter: func(t *testing.T, stateDir, _ string) {
			writeFixture(t, filepath.Join(transactionDir(stateDir), "unrecognized"), "preserve me", 0o600)
		}},
		{name: "sibling residue", publish: 2, want: ErrAbandonedFileSetResidue, alter: func(t *testing.T, stateDir, _ string) {
			writeFixture(t, filepath.Join(stateDir, ".daem-tombstone-11111111111111111111111111111111", "residue"), "preserve me", 0o600)
		}},
		{name: "changed target", publish: 2, want: mutation.StaleSnapshotError{}, revalidate: true, alter: func(t *testing.T, _, target string) {
			writeFixture(t, target, "later edit", 0o640)
		}},
		{name: "changed backup", publish: 2, want: mutation.StaleSnapshotError{}, revalidate: true, alter: func(t *testing.T, stateDir, _ string) {
			writeFixture(t, filepath.Join(transactionDir(stateDir), "target-000.before"), "later edit", 0o600)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stateDir, manifest, lockfile, marker := setupFinalizationPair(t, false, false)
			publishFinalizationImages(t, marker, test.publish)
			if test.alter != nil && !test.revalidate {
				test.alter(t, stateDir, manifest)
			}
			read := func(path string) string {
				t.Helper()
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				return string(content)
			}
			wantManifest, wantLock, wantMarker := read(manifest), read(lockfile), read(markerPath(stateDir))
			retired, err := FinalizeCommittedPair(t.Context(), stateDir, manifest, lockfile, func(context.Context) error {
				if test.revalidate {
					test.alter(t, stateDir, manifest)
					wantManifest = read(manifest)
				}
				return nil
			})
			if retired || !errors.Is(err, test.want) {
				t.Fatalf("retired = %t; error = %v, want %v", retired, err, test.want)
			}
			assertContent(t, manifest, wantManifest)
			assertContent(t, lockfile, wantLock)
			assertContent(t, markerPath(stateDir), wantMarker)
		})
	}
}

func setupFinalizationPair(t *testing.T, retain, create bool) (string, string, string, transactionMarker) {
	t.Helper()
	root := t.TempDir()
	stateDir := mustCanonicalStateDir(t, filepath.Join(root, ".daem"))
	manifest := filepath.Join(root, "daem.toml")
	lockfile := filepath.Join(root, "daem.lock.toml")
	writeFixture(t, manifest, "manifest before", 0o640)
	if !create {
		writeFixture(t, lockfile, "lock before", 0o600)
	}
	companion := mustWriteTarget(t, lockfile, "lock after")
	if retain {
		companion = mustRetainedTarget(t, lockfile)
	}
	replacement := mustWriteTarget(t, manifest, "manifest after")
	targets, err := canonicalTargets([]FileTarget{replacement, companion})
	if err != nil {
		t.Fatal(err)
	}
	marker, err := prepareMarker(t.Context(), stateDir, targets)
	if err != nil {
		t.Fatal(err)
	}
	return stateDir, replacement.path, companion.path, marker
}

func publishFinalizationImages(t *testing.T, marker transactionMarker, count int) {
	t.Helper()
	for _, target := range marker.Targets[:count] {
		if !target.Write {
			continue
		}
		content := "manifest after"
		if filepath.Base(target.Path) == "daem.lock.toml" {
			content = "lock after"
		}
		if err := commitFile(t.Context(), target.Path, []byte(content), fileMode(target.Before)); err != nil {
			t.Fatal(err)
		}
	}
}

func statFinalizationPath(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
