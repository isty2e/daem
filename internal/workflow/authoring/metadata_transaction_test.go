package authoring

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/effect/journal"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestAuthoringPreviewsRefuseInterruptedMetadataTransaction(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(
		t,
		paths.ManifestPath,
		[]byte(unmanageManifest("context7@official", target.ScopeProject)),
	)
	metadatatx.WriteInterrupted(t, paths.StateDir)

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "remove",
			run: func() error {
				_, err := RemoveExtension(
					context.Background(),
					ExecutionOptions{
						ManifestPath: paths.ManifestPath,
						Mode:         AuthoringModeDryRun,
					},
					RemoveExtensionRequest{ID: "context7"},
				)
				return err
			},
		},
		{
			name: "unmanage",
			run: func() error {
				_, err := UnmanageExtension(
					context.Background(),
					UnmanageExtensionRequest{
						ManifestPath: paths.ManifestPath,
						ID:           "context7",
						Mode:         UnmanageModeDryRun,
					},
				)
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil || !strings.Contains(err.Error(), "interrupted file-set transaction") {
				t.Fatalf("error = %v, want interrupted file-set transaction", err)
			}
		})
	}
}

func TestAuthoringWritesRecoverMatchingEvidenceBeforeReading(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, daempaths.Paths) error
	}{
		{
			name: "remove",
			run: func(ctx context.Context, paths daempaths.Paths) error {
				_, err := RemoveExtension(
					ctx,
					ExecutionOptions{
						ManifestPath: paths.ManifestPath,
						Mode:         AuthoringModeWrite,
					},
					RemoveExtensionRequest{ID: "context7"},
				)
				return err
			},
		},
		{
			name: "unmanage",
			run: func(ctx context.Context, paths daempaths.Paths) error {
				_, err := UnmanageExtension(ctx, UnmanageExtensionRequest{
					ManifestPath: paths.ManifestPath,
					ID:           "context7",
					Mode:         UnmanageModeWrite,
				})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configureUnmanageTestHomes(t, root)
			paths := unmanageTestPaths(t, root)
			writeUnmanageFile(
				t,
				paths.ManifestPath,
				[]byte(unmanageManifest("context7@official", target.ScopeProject)),
			)
			metadatatx.WriteInterruptedForAbsentTarget(
				t,
				paths.StateDir,
				paths.LockfilePath,
			)

			if err := test.run(context.Background(), paths); err != nil {
				t.Fatalf("write returned error: %v", err)
			}
			authorityPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(authorityPath); !os.IsNotExist(err) {
				t.Fatalf("metadata transaction evidence remains: %v", err)
			}
		})
	}
}

func TestAuthoringMutationRefusesActiveJournalCreatedDuringRebuild(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	original := []byte(unmanageManifest("context7@official", target.ScopeProject))
	writeUnmanageFile(t, paths.ManifestPath, original)

	buildCalls := 0
	_, err := executeAuthoringOperation(
		t.Context(),
		ExecutionOptions{ManifestPath: paths.ManifestPath, Mode: AuthoringModeWrite},
		func(document ManifestDocument) (Change, error) {
			buildCalls++
			if buildCalls == 2 {
				captureUnmanageActiveJournal(t, paths)
			}
			return BuildRemoveExtensionChange(document, RemoveExtensionRequest{ID: "context7"})
		},
	)
	if !errors.Is(err, journal.ErrInterruptedApply) {
		t.Fatalf("executeAuthoringOperation error = %v, want interrupted apply", err)
	}
	content, readErr := os.ReadFile(paths.ManifestPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("manifest changed after refused authoring mutation:\n%s", content)
	}
	if buildCalls != 2 {
		t.Fatalf("build calls = %d, want initial and revalidation builds", buildCalls)
	}
}

func TestAuthoringWriteRefusesIncompleteEvidenceBeforeManifestRead(t *testing.T) {
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, root)
	writeUnmanageFile(t, paths.ManifestPath, []byte("[invalid\n"))
	authorityPath, err := transaction.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authorityPath, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err = RemoveExtension(
		context.Background(),
		ExecutionOptions{
			ManifestPath: paths.ManifestPath,
			Mode:         AuthoringModeWrite,
		},
		RemoveExtensionRequest{ID: "context7"},
	)
	if err == nil || !strings.Contains(err.Error(), "marker is missing") {
		t.Fatalf("error = %v, want incomplete evidence refusal", err)
	}
}
