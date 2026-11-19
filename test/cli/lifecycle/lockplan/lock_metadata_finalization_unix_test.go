//go:build darwin || linux

package cli_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

const minimalFinalizationManifest = "version = 1\ntargets = [\"codex\"]\n"

func TestLockFinalizesOnlyItsSelectedPair(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	alias := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(alias, "other.toml")
	lock := filepath.Join(root, "daem.lock.toml")
	testkit.WriteFile(t, root, "daem.toml", minimalFinalizationManifest)
	lockReferent := filepath.Join(root, "linked", "lock.toml")
	if err := os.Symlink(lockReferent, lock); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	settledManifest := minimalFinalizationManifest + "\n[instructions.project]\nsource = 'source.md'\n"
	testkit.WriteFile(t, root, "source.md", "current source content\n")
	legacyLock := "version = 6\n"
	marker := seedInterruptedLockPair(t, paths.StateDir, manifest, lockReferent, settledManifest, legacyLock, true)

	preview := runMetadataLock(t, 1, "--dry-run", "--manifest", manifest)
	requireMetadataGuidance(t, preview, "preview leaves the recovery record untouched")
	foreign := runMetadataLock(t, 1)
	requireMetadataGuidance(t, foreign, "Manifest Metadata Update Was Interrupted")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("refusal did not preserve marker: %v", err)
	}
	testkit.AssertFileContent(t, lockReferent, legacyLock)

	success := runMetadataLock(t, 0, "--manifest", manifest)
	result := clijson.DecodeLock(t, []byte(success))
	if result.PreviousStatus != "legacy" || result.Mode != "write" || result.ManifestPath != manifest || result.EntryCounts.Subjects == 0 {
		t.Fatalf("unexpected lock result after retirement: %s", success)
	}
	assertRetiredLockMarker(t, marker)
	testkit.AssertFileContent(t, manifest, settledManifest)
	testkit.AssertFileContent(t, filepath.Join(root, "daem.toml"), minimalFinalizationManifest)
	if target, err := os.Readlink(lock); err != nil || target != lockReferent {
		t.Fatalf("lockfile link = %q, %v", target, err)
	}
	if _, err := lockfile.Load(t.Context(), lockReferent); err != nil {
		t.Fatalf("normal lock did not publish a readable current lockfile: %v", err)
	}
}

func TestLockFinalizationFailureAndContinuationAreDistinct(t *testing.T) {
	for _, test := range []struct {
		name        string
		finished    bool
		userDefault bool
		journal     bool
		want        string
	}{
		{name: "mixed", want: "retry the exact interrupted authoring command"},
		{name: "unknown journal", finished: true, journal: true},
		{name: "source fails after retirement", finished: true, want: "recovery record was already cleared"},
		{name: "implicit user selection", finished: true, userDefault: true, want: "recovery record was already cleared"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			selection := ""
			if !test.userDefault {
				selection = filepath.Join(root, "daem.toml")
			}
			paths, err := daempaths.Resolve(selection)
			if err != nil {
				t.Fatal(err)
			}
			manifest := minimalFinalizationManifest + "\n[instructions.project]\nsource = 'source.md'\n"
			if test.userDefault {
				testkit.WriteFile(t, paths.ManifestRoot, "source.md", "this exists; only the implicit project placement must fail\n")
			}
			oldLock := "version = 6\n"
			marker := seedInterruptedLockPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath, manifest, oldLock, test.finished)
			if test.journal {
				testkit.WriteFile(t, paths.StateDir, "recovery", "unreadable as a journal directory")
			}
			failure := runMetadataLock(t, 1)
			if test.want != "" {
				requireMetadataGuidance(t, failure, test.want)
			}
			if test.finished && !test.journal {
				assertRetiredLockMarker(t, marker)
				payload := clijson.DecodeFailure(t, failure, "lock", "operation")
				wantCause := "source.md"
				if test.userDefault {
					wantCause = "requires a project manifest"
				}
				if !strings.Contains(payload.Error.Message, wantCause) {
					t.Fatalf("lock error = %q, want %q", payload.Error.Message, wantCause)
				}
			} else if _, err := os.Stat(marker); err != nil {
				t.Fatalf("refusal did not preserve marker: %v", err)
			}
			testkit.AssertFileContent(t, paths.ManifestPath, manifest)
			if !test.finished {
				oldLock += "# before\n"
			}
			testkit.AssertFileContent(t, paths.LockfilePath, oldLock)
		})
	}
}

func TestLockFinalizationDoesNotConsumeNewFenceDuringNormalLock(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	oldLock := "version = 6\n"
	marker := seedInterruptedLockPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath, minimalFinalizationManifest, oldLock, true)
	directory := filepath.Dir(marker)
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	evidence := make(map[string]string, len(entries))
	for _, entry := range entries {
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		evidence[entry.Name()] = string(content)
	}

	_, err = workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: paths.ManifestPath,
		PrepareResult: func(workflowlock.Result) error {
			assertRetiredLockMarker(t, marker)
			for name, content := range evidence {
				testkit.WriteFile(t, directory, name, content)
			}
			return nil
		},
	})
	var stale mutation.StaleSnapshotError
	if !errors.As(err, &stale) || workflowlock.MetadataRecoveryStatusOf(err) != workflowlock.MetadataRecoveryRetired {
		t.Fatalf("expected a new fence refusal after confirmed retirement: %v", err)
	}
	for name, content := range evidence {
		testkit.AssertFileContent(t, filepath.Join(directory, name), content)
	}
	testkit.AssertFileContent(t, paths.LockfilePath, oldLock)
}

// The writer-side tests use the real staged marker; this fixture exercises a
// previously released wire record through the command, without a timing race.
func seedInterruptedLockPair(t *testing.T, stateDir, manifest, lock, manifestAfter, lockAfter string, finished bool) string {
	t.Helper()
	type beforeImage struct {
		Exists bool   `json:"exists"`
		Hash   string `json:"hash"`
		Path   string `json:"backup_path"`
		Mode   uint32 `json:"mode"`
	}
	type row struct {
		Path  string      `json:"path"`
		Old   beforeImage `json:"before"`
		New   string      `json:"after_hash"`
		Write bool        `json:"write"`
	}
	canonical := func(path string) string {
		t.Helper()
		resolved, err := mutation.CanonicalDirectoryEntryPath(path)
		if err != nil {
			t.Fatal(err)
		}
		return resolved
	}
	evidence := testkit.AuthoringTransactionDir(canonical(stateDir))
	hash := func(content string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))) }
	manifest, lock = canonical(manifest), canonical(lock)
	after := map[string]string{manifest: manifestAfter, lock: lockAfter}
	files := []string{manifest, lock}
	slices.Sort(files)
	rows := make([]row, 0, len(files))
	for index, path := range files {
		before := after[path] + "# before\n"
		name := fmt.Sprintf("target-%03d.before", index)
		testkit.WriteFile(t, evidence, name, before)
		rows = append(rows, row{Path: path, Old: beforeImage{true, hash(before), filepath.Join(evidence, name), 0o600}, New: hash(after[path]), Write: true})
		content := after[path]
		if path == lock && !finished {
			content = before
		}
		testkit.WriteFile(t, filepath.Dir(path), filepath.Base(path), content)
	}
	marker, err := json.Marshal(struct {
		Version int   `json:"version"`
		Targets []row `json:"targets"`
	}{contractversion.MetadataTransaction, rows})
	if err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, evidence, "transaction.json", string(marker))
	return filepath.Join(evidence, "transaction.json")
}

func runMetadataLock(t *testing.T, want int, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	argv := append([]string{"lock", "--json"}, args...)
	if code := testkit.RunCLI(argv, &stdout, &stderr); code != want || stderr.Len() != 0 {
		t.Fatalf("%v exit = %d, want %d\nstdout = %s\nstderr = %s", argv, code, want, &stdout, &stderr)
	}
	return stdout.String()
}

func requireMetadataGuidance(t *testing.T, output, want string) {
	t.Helper()
	diagnostic := clijson.DecodeFailure(t, output, "lock", "operation")
	for _, item := range diagnostic.Guidance {
		if strings.Contains(item.Message, want) {
			return
		}
	}
	t.Fatalf("guidance = %+v, want %q", diagnostic.Guidance, want)
}

func assertRetiredLockMarker(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Dir(marker)); !os.IsNotExist(err) {
		t.Fatalf("marker directory should be absent: %v", err)
	}
}
