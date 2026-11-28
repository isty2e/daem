//go:build darwin || linux

package cli_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestLockPreviewUsesVisibleFilesDuringPartialDeclarationWrite(t *testing.T) {
	testkit.RequireGit(t)
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	repo := testkit.InitGitRepository(t, root)
	testkit.WriteFile(t, repo, "notes.md", "instructions from Git\n")
	commit := testkit.CommitRepository(t, repo, "add project instructions")
	// The manifest sorts before the lockfile, so writing one target makes
	// the new declaration visible while leaving the old lockfile in place.
	workspace, err := paths.Resolve(filepath.Join(root, "alternate.toml"))
	if err != nil {
		t.Fatal(err)
	}
	visibleManifest := minimalFinalizationManifest +
		fmt.Sprintf("\n[instructions.project]\nsource = { git = %q, path = 'notes.md', ref = 'main' }\n", repo)
	previousLock, err := lockfile.Marshal(testkit.ExactSupplyLockfile(t))
	if err != nil {
		t.Fatal(err)
	}
	metadatatx.WritePrefix(t, workspace.StateDir, []metadatatx.Write{
		{Path: workspace.ManifestPath, Before: &metadatatx.Image{Content: []byte(minimalFinalizationManifest), Mode: 0o600}, After: []byte(visibleManifest)},
		{Path: workspace.LockfilePath, Before: &metadatatx.Image{Content: previousLock, Mode: 0o600}, After: []byte("unpublished lockfile\n")},
	}, 1)
	evidence, err := fileset.FileSetAuthorityPath(workspace.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(evidence, "transaction.json")
	markerBytes := testkit.ReadFile(t, marker)

	payload := runMetadataLock(t, 0, "--dry-run", "--manifest", workspace.ManifestPath)
	preview := clijson.DecodeLock(t, []byte(payload))
	if preview.Mode != "dry-run" || preview.PreviousStatus != "comparable" || preview.HasChanges == nil || !*preview.HasChanges ||
		!slices.ContainsFunc(preview.SubjectChanges, func(change clijson.LockSubjectChange) bool {
			return change.Status == "added" && change.After != nil && change.After.ExactSupply != nil && change.After.ExactSupply.ResolvedRef == commit
		}) {
		t.Fatalf("preview did not compare the current lock against the visible Git source: %s", payload)
	}

	testkit.AssertFileContent(t, workspace.ManifestPath, visibleManifest)
	testkit.AssertFileContent(t, workspace.LockfilePath, string(previousLock))
	testkit.AssertFileContent(t, marker, string(markerBytes))
	if _, err := os.Lstat(workspace.SourceCacheDir); !os.IsNotExist(err) {
		t.Fatalf("preview used a persistent cache: %v", err)
	}
}
