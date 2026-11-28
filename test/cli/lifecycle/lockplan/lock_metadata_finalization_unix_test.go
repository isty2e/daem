//go:build darwin || linux

package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/metadatatx"
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
	marker := metadatatx.WriteInterruptedPair(t, paths.StateDir, manifest, lockReferent, settledManifest, legacyLock, true)
	markerBytes := testkit.ReadFile(t, marker)

	preview := clijson.DecodeLock(t, []byte(runMetadataLock(t, 0, "--dry-run", "--manifest", manifest)))
	if preview.PreviousStatus != "legacy" || preview.Mode != "dry-run" || preview.EntryCounts.Subjects == 0 {
		t.Fatalf("unexpected preview of the completed pair: %+v", preview)
	}
	testkit.AssertFileContent(t, manifest, settledManifest)
	testkit.AssertFileContent(t, lockReferent, legacyLock)
	testkit.AssertFileContent(t, marker, string(markerBytes))

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
		global      bool
		want        string
	}{
		{name: "mixed"},
		{name: "unknown journal", finished: true, journal: true},
		{name: "global metadata fence", finished: true, global: true},
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
			marker := metadatatx.WriteInterruptedPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath, manifest, oldLock, test.finished)
			if test.journal {
				testkit.WriteFile(t, paths.StateDir, "recovery", "unreadable as a journal directory")
			}
			if test.global {
				metadatatx.WriteInterrupted(t, paths.GlobalMetadataDir())
			}
			if !test.finished {
				preview := clijson.DecodeFailure(t, runMetadataLock(t, 1, "--dry-run"), "lock", "operation")
				if !slices.ContainsFunc(preview.Guidance, func(item clijson.FailureGuidance) bool {
					return slices.Equal(item.Argv, []string{"daem", "lock", "--dry-run"})
				}) {
					t.Fatalf("preview did not reach the missing source: %+v", preview)
				}
			}
			failure := runMetadataLock(t, 1)
			if test.global {
				payload := clijson.DecodeFailure(t, failure, "lock", "operation")
				if !slices.ContainsFunc(payload.Guidance, func(item clijson.FailureGuidance) bool {
					return slices.Equal(item.Argv, []string{"daem", "migrate", "metadata", "--recover", "--dry-run"})
				}) {
					t.Fatalf("did not retain global recovery guidance: %+v", payload.Guidance)
				}
			}
			if test.want != "" {
				requireMetadataGuidance(t, failure, test.want)
			}
			if !test.finished {
				payload := clijson.DecodeFailure(t, failure, "lock", "operation")
				if !slices.ContainsFunc(payload.Guidance, func(item clijson.FailureGuidance) bool {
					return slices.Equal(item.Argv, []string{"daem", "recover", "--declarations", "--dry-run"})
				}) {
					t.Fatalf("partial pair did not point to declaration recovery: %+v", payload.Guidance)
				}
			}
			if test.finished && !test.journal && !test.global {
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

func TestLockFinalizationRefusesMissingRecordedFiles(t *testing.T) {
	for _, name := range []string{"daem.toml", "daem.lock.toml"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			manifest, lock := filepath.Join(root, "daem.toml"), filepath.Join(root, "daem.lock.toml")
			marker := metadatatx.WriteInterruptedPair(t, filepath.Join(root, ".daem"), manifest, lock, minimalFinalizationManifest, "version = 6\n", true)
			missing := filepath.Join(root, name)
			if err := os.Remove(missing); err != nil {
				t.Fatal(err)
			}
			failure := runMetadataLock(t, 1, "--manifest", manifest)
			payload := clijson.DecodeFailure(t, failure, "lock", "operation")
			if !strings.Contains(payload.Error.Message, "interrupted file-set transaction") ||
				slices.ContainsFunc(payload.Guidance, func(item clijson.FailureGuidance) bool { return slices.Contains(item.Argv, "init") }) {
				t.Fatalf("did not preserve the interrupted-write diagnosis: %+v", payload)
			}
			requireMetadataGuidance(t, failure, "Manifest Metadata Update Was Interrupted")
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("refusal did not preserve marker: %v", err)
			}
			if _, err := os.Stat(missing); !os.IsNotExist(err) {
				t.Fatalf("refusal recreated the missing file: %v", err)
			}
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
	marker := metadatatx.WriteInterruptedPair(t, paths.StateDir, paths.ManifestPath, paths.LockfilePath, minimalFinalizationManifest, oldLock, true)
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
