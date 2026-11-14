package transaction

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

func TestSourcePublicationWritesSelectedReferentsAndPreservesLinksAndModes(t *testing.T) {
	root := t.TempDir()
	manifest := filepath.Join(root, "daem.toml")
	source := filepath.Join(root, "source.json")
	realManifest, realSource := filepath.Join(root, "real-manifest"), filepath.Join(root, "real-source")
	writeAdapterFixture(t, realManifest, "manifest-before", 0o640)
	writeAdapterFixture(t, realSource, "source-before", 0o600)
	if err := os.Symlink("real-manifest", manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real-source", source); err != nil {
		t.Fatal(err)
	}
	input := SourceTransactionInput{
		ManifestTarget: captureAdapterTarget(t, manifest), SourceTarget: captureAdapterTarget(t, source),
		StateDir: filepath.Join(root, ".daem"), ManifestBytes: []byte("manifest-after"), SourceBytes: []byte("source-after"),
	}
	if err := CommitSourceTransaction(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	assertAdapterContent(t, realManifest, "manifest-after")
	assertAdapterContent(t, realSource, "source-after")
	for path, mode := range map[string]os.FileMode{realManifest: 0o640, realSource: 0o600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatal("source publication lost permissions", path, err)
		}
	}
	for path, target := range map[string]string{manifest: "real-manifest", source: "real-source"} {
		if actual, err := os.Readlink(path); err != nil || actual != target {
			t.Fatal("source publication replaced a selected link", err)
		}
	}
}

func TestSourcePublicationRejectsStaleInputsBeforeAnyWrite(t *testing.T) {
	for _, staleSource := range []bool{false, true} {
		t.Run(map[bool]string{false: "manifest", true: "source"}[staleSource], func(t *testing.T) {
			root := t.TempDir()
			manifest, source := filepath.Join(root, "daem.toml"), filepath.Join(root, "source.json")
			writeAdapterFixture(t, manifest, "manifest-before", 0o600)
			writeAdapterFixture(t, source, "source-before", 0o600)
			input := SourceTransactionInput{
				ManifestTarget: captureAdapterTarget(t, manifest), SourceTarget: captureAdapterTarget(t, source),
				StateDir: filepath.Join(root, ".daem"), ManifestBytes: []byte("must-not-write"), SourceBytes: []byte("must-not-write"),
			}
			changed := manifest
			if staleSource {
				changed = source
			}
			writeAdapterFixture(t, changed, "external-edit", 0o600)
			err := CommitSourceTransaction(t.Context(), input)
			var stale mutation.StaleSnapshotError
			if !errors.As(err, &stale) {
				t.Fatal("stale source publication was not refused", err)
			}
			assertAdapterContent(t, changed, "external-edit")
			unchanged, expected := source, "source-before"
			if staleSource {
				unchanged, expected = manifest, "manifest-before"
			}
			assertAdapterContent(t, unchanged, expected)
			if _, err := os.Stat(input.StateDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("stale refusal published transaction evidence", err)
			}
		})
	}
}

func TestSourcePublicationCreatesMissingTargetsAndRetirementLeavesSourceAlone(t *testing.T) {
	root := t.TempDir()
	manifest, source := filepath.Join(root, "daem.toml"), filepath.Join(root, "daem.d", "source.json")
	input := SourceTransactionInput{
		ManifestTarget: captureAdapterTarget(t, manifest), SourceTarget: captureAdapterTarget(t, source),
		StateDir: filepath.Join(root, ".daem"), ManifestBytes: []byte("manifest-after"), SourceBytes: []byte("source-after"),
	}
	if err := CommitSourceTransaction(t.Context(), input); err != nil {
		t.Fatal(err)
	}
	assertAdapterContent(t, manifest, "manifest-after")
	assertAdapterContent(t, source, "source-after")
	writeAdapterFixture(t, source, "external-source-edit", 0o400)
	if err := CommitSourceRetirement(t.Context(), SourceRetirementInput{
		ManifestTarget: captureAdapterTarget(t, manifest), StateDir: input.StateDir, ManifestBytes: []byte("without-declaration"),
	}); err != nil {
		t.Fatal(err)
	}
	assertAdapterContent(t, manifest, "without-declaration")
	assertAdapterContent(t, source, "external-source-edit")
	info, err := os.Stat(source)
	if err != nil || info.Mode().Perm() != 0o400 {
		t.Fatal("retirement changed the untouched source", err)
	}
}

func TestSourcePublicationCannotRecoverAndStartAnotherCapture(t *testing.T) {
	for _, retirement := range []bool{false, true} {
		for _, finalized := range []bool{false, true} {
			fixture := newSourceRecoveryFixture(t, sourceRecoverySetup{retirement: retirement, finalized: finalized})
			manifest := captureAdapterTarget(t, fixture.manifest)
			var err error
			if retirement {
				err = CommitSourceRetirement(t.Context(), SourceRetirementInput{
					ManifestTarget: manifest, StateDir: fixture.stateDir, ManifestBytes: []byte("recaptured"),
				})
			} else {
				err = CommitSourceTransaction(t.Context(), SourceTransactionInput{
					ManifestTarget: manifest, SourceTarget: captureAdapterTarget(t, fixture.source),
					StateDir: fixture.stateDir, ManifestBytes: []byte("recaptured"), SourceBytes: []byte("recaptured"),
				})
			}
			if !errors.Is(err, fileset.ErrInterruptedFileSetTransaction) {
				t.Fatal("publication recovered and started a new capture", retirement, finalized, err)
			}
			assertAdapterContent(t, fixture.source, "source-after")
			manifestContent := "manifest-before"
			if finalized {
				manifestContent = "manifest-after"
			}
			assertAdapterContent(t, fixture.manifest, manifestContent)
			if _, err := os.Stat(fixture.marker); err != nil {
				t.Fatal("publication consumed the pending recovery", err)
			}
		}
	}
}

func TestSourcePublicationRejectsEquivalentContentSourceRetarget(t *testing.T) {
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	source, manifest := filepath.Join(root, "source"), filepath.Join(root, "daem.toml")
	writeAdapterFixture(t, first, "same", 0o600)
	writeAdapterFixture(t, second, "same", 0o600)
	writeAdapterFixture(t, manifest, "manifest-before", 0o600)
	if err := os.Symlink("first", source); err != nil {
		t.Fatal(err)
	}
	input := SourceTransactionInput{
		ManifestTarget: captureAdapterTarget(t, manifest), SourceTarget: captureAdapterTarget(t, source),
		StateDir: filepath.Join(root, ".daem"), ManifestBytes: []byte("must-not-write"), SourceBytes: []byte("must-not-write"),
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second", source); err != nil {
		t.Fatal(err)
	}
	err := CommitSourceTransaction(t.Context(), input)
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) {
		t.Fatal("source retarget was mistaken for the approved source", err)
	}
	assertAdapterContent(t, first, "same")
	assertAdapterContent(t, second, "same")
	assertAdapterContent(t, manifest, "manifest-before")
}
