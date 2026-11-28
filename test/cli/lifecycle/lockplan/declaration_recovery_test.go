//go:build darwin || linux

package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/metadatatx"
)

func TestDeclarationRecoveryRollsBackWithoutReplaying(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	workspace, err := paths.Resolve(filepath.Join(root, "my manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	interrupted := minimalFinalizationManifest + "\n[instructions.interrupted]\nsource = 'missing.md'\n"
	metadatatx.WritePrefix(t, workspace.StateDir, []metadatatx.Write{
		{Path: workspace.ManifestPath, Before: declarationImage(minimalFinalizationManifest), After: []byte(interrupted)},
		{Path: workspace.LockfilePath, After: []byte("interrupted lock bytes\n")},
	}, 1)
	marker := declarationMarker(t, workspace)
	before := testkit.ReadFile(t, marker)

	lock := runMetadataLock(t, 1, "--manifest", workspace.ManifestPath)
	blocked := clijson.DecodeFailure(t, lock, "lock", "operation")
	if !slices.ContainsFunc(blocked.Guidance, func(item clijson.FailureGuidance) bool {
		return slices.Equal(item.Argv, []string{"daem", "recover", "--manifest", workspace.ManifestPath, "--declarations", "--dry-run"})
	}) {
		t.Fatalf("lock refusal did not preserve the manifest selector: %+v", blocked.Guidance)
	}
	preview := runDeclarationRecoveryCLI(t, workspace.ManifestPath, 0, "--dry-run")
	assertDeclarationRecoveryJSON(t, preview, "dry-run", "planned", "rollback", true)
	testkit.AssertFileContent(t, workspace.ManifestPath, minimalFinalizationManifest)
	testkit.AssertFileContent(t, workspace.LockfilePath, "interrupted lock bytes\n")
	testkit.AssertFileContent(t, marker, string(before))

	written := runDeclarationRecoveryCLI(t, workspace.ManifestPath, 0, "--yes")
	assertDeclarationRecoveryJSON(t, written, "write", "completed", "rollback", true)
	assertRetiredLockMarker(t, marker)
	testkit.AssertFileContent(t, workspace.ManifestPath, minimalFinalizationManifest)
	assertMissingDeclarationFile(t, workspace.LockfilePath)
	assertMissingDeclarationFile(t, workspace.StatefilePath)

	testkit.WriteFile(t, root, "note.md", "new edit\n")
	var stdout, stderr bytes.Buffer
	args := []string{"add", "instruction", "new", "note.md", "--manifest", workspace.ManifestPath, "--target", "codex"}
	if code := testkit.RunCLI(args, &stdout, &stderr); code != 0 {
		t.Fatalf("new edit against restored manifest failed: code=%d, stdout=%s, stderr=%s", code, &stdout, &stderr)
	}
	environment, err := declarationmanifest.Decode(testkit.ReadFile(t, workspace.ManifestPath))
	if err != nil {
		t.Fatal(err)
	}
	instructions := environment.Instructions()
	if len(instructions) != 1 || instructions[0].ID().Name() != "new" {
		t.Fatalf("the new manifest did not contain only the requested edit: %v", instructions)
	}
}

func TestDeclarationRecoveryRetiresCompletedLinkedPairWithoutReplacingFiles(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	workspace, err := paths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "declared.toml")
	lockfile := filepath.Join(root, "locked.toml")
	for selected, referent := range map[string]string{workspace.ManifestPath: manifest, workspace.LockfilePath: lockfile} {
		if err := os.Symlink(filepath.Base(referent), selected); err != nil {
			t.Fatal(err)
		}
	}
	marker := metadatatx.WriteInterruptedPair(t, workspace.StateDir, manifest, lockfile, "completed manifest bytes\n", "completed lock bytes\n", true)
	beforeManifest := statDeclarationPath(t, manifest)
	beforeLock := statDeclarationPath(t, lockfile)

	preview := runDeclarationRecoveryCLI(t, workspace.ManifestPath, 0, "--dry-run")
	assertDeclarationRecoveryJSON(t, preview, "dry-run", "planned", "finalize", false)
	_ = statDeclarationPath(t, marker)
	written := runDeclarationRecoveryCLI(t, workspace.ManifestPath, 0, "--yes")
	assertDeclarationRecoveryJSON(t, written, "write", "completed", "finalize", false)
	assertRetiredLockMarker(t, marker)
	if !os.SameFile(beforeManifest, statDeclarationPath(t, workspace.ManifestPath)) ||
		!os.SameFile(beforeLock, statDeclarationPath(t, workspace.LockfilePath)) {
		t.Fatal("cleanup replaced a selected declaration file")
	}
	testkit.AssertFileContent(t, manifest, "completed manifest bytes\n")
	testkit.AssertFileContent(t, lockfile, "completed lock bytes\n")
	for selected, referent := range map[string]string{workspace.ManifestPath: manifest, workspace.LockfilePath: lockfile} {
		if target, err := os.Readlink(selected); err != nil || target != filepath.Base(referent) {
			t.Fatalf("selection %q no longer names the same link: %q, %v", selected, target, err)
		}
	}
}

func TestDeclarationRecoveryDoesNotBorrowOtherTransactionTargets(t *testing.T) {
	for _, scenario := range []string{"sibling manifest", "retained state", "lock aliases state", "unrecognized contents"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			workspace, err := paths.Resolve(filepath.Join(root, "daem.toml"))
			if err != nil {
				t.Fatal(err)
			}
			manifest, lockfile := workspace.ManifestPath, workspace.LockfilePath
			switch scenario {
			case "sibling manifest":
				manifest = filepath.Join(root, "sibling.toml")
				testkit.WriteFile(t, root, "daem.toml", minimalFinalizationManifest)
			case "lock aliases state":
				lockfile = workspace.StatefilePath
				if err := os.Symlink(lockfile, workspace.LockfilePath); err != nil {
					t.Fatal(err)
				}
			}
			writes := []metadatatx.Write{
				{Path: manifest, Before: declarationImage("manifest before\n"), After: []byte("manifest after\n")},
				{Path: lockfile, Before: declarationImage("lock before\n"), After: []byte("lock after\n")},
			}
			if scenario == "retained state" {
				writes = append(writes, metadatatx.Write{Path: workspace.StatefilePath, Retain: true})
			}
			metadatatx.WritePrefix(t, workspace.StateDir, writes, 1)
			if scenario == "unrecognized contents" {
				testkit.WriteFile(t, root, "daem.toml", "external edit\n")
			}
			marker := declarationMarker(t, workspace)
			contents := make(map[string]string)
			for _, path := range []string{manifest, lockfile, workspace.ManifestPath, marker} {
				contents[path] = string(testkit.ReadFile(t, path))
			}

			failure := runDeclarationRecoveryCLI(t, workspace.ManifestPath, 1, "--yes")
			_ = clijson.DecodeFailure(t, failure, "recover", "operation")
			for path, content := range contents {
				testkit.AssertFileContent(t, path, content)
			}
		})
	}
}

func declarationImage(text string) *metadatatx.Image {
	return &metadatatx.Image{Content: []byte(text), Mode: 0o600}
}

func declarationMarker(t *testing.T, workspace paths.Paths) string {
	t.Helper()
	dir, err := fileset.FileSetAuthorityPath(workspace.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "transaction.json")
}

func runDeclarationRecoveryCLI(t *testing.T, manifest string, exit int, mode string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	args := []string{"recover", "--declarations", "--manifest", manifest, mode, "--json"}
	if code := testkit.RunCLI(args, &stdout, &stderr); code != exit || stderr.Len() != 0 {
		t.Fatalf("%v: exit=%d, want %d; stdout=%s stderr=%s", args, code, exit, &stdout, &stderr)
	}
	return stdout.String()
}

func assertDeclarationRecoveryJSON(t *testing.T, output, mode, phase, action string, removesLockfile bool) {
	t.Helper()
	result := clijson.DecodeRecover(t, []byte(output))
	if result.SchemaVersion != contractversion.RecoveryJSON || result.Command != "recover" ||
		result.Mode != mode || result.AuthorityKind != "declaration_transaction" || result.Phase != phase ||
		result.Action != action || result.RemovesLockfile != removesLockfile || result.HasErrors {
		t.Fatalf("unexpected recovery result: %s", output)
	}
}

func assertMissingDeclarationFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %q to be absent, got %v", path, err)
	}
}

func statDeclarationPath(t *testing.T, path string) os.FileInfo {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info
}
