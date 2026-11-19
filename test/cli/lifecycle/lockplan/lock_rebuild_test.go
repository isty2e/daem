package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestLockRebuildsDamagedCurrentLockWithoutChangingManagedState(t *testing.T) {
	root := t.TempDir()
	testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
	manifest := filepath.Join(root, "daem.toml")
	lockPath := filepath.Join(root, "daem.lock.toml")
	testkit.WriteFile(t, root, "daem.toml", `version = 1
targets = ["codex"]

[instructions.project]
source = "source.md"
`)
	testkit.WriteFile(t, root, "source.md", "project instructions\n")
	run := func(wantCode int, args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		args = append(args, "--manifest", manifest)
		if code := testkit.RunCLI(args, &stdout, &stderr); code != wantCode {
			t.Fatalf("%v: exit = %d, want %d\nstdout: %s\nstderr: %s", args, code, wantCode, &stdout, &stderr)
		}
		return stdout.String()
	}
	read := func(path string) string {
		t.Helper()
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}

	run(0, "lock")
	run(0, "apply", "--yes")
	run(0, "status", "--check")
	before := make(map[string]string)
	for _, path := range []string{"daem.toml", "source.md", "AGENTS.md", ".daem/state.json"} {
		before[path] = read(path)
	}
	canonical := read("daem.lock.toml")
	damaged := fmt.Sprintf("version = %d\n[[\n", contractversion.LockfileSchema)
	if err := os.WriteFile(lockPath, []byte(damaged), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o640); err != nil {
		t.Fatal(err)
	}
	run(1, "apply", "--dry-run")
	run(1, "status", "--check")
	run(1, "outdated")
	preview := requireUnavailableLockComparison(t, run(0, "lock", "--dry-run", "--json"), "damaged", "dry-run")
	testkit.AssertFileContent(t, lockPath, damaged)
	if info, err := os.Stat(lockPath); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("lock mode after preview: %v, %v", info, err)
	}

	written := requireUnavailableLockComparison(t, run(0, "lock", "--json"), "damaged", "write")
	if written.EntryCounts != preview.EntryCounts || written.ManifestPath != preview.ManifestPath || written.LockfilePath != preview.LockfilePath {
		t.Fatal("preview and write differ on fixed inputs")
	}
	testkit.AssertFileContent(t, lockPath, canonical)
	if _, err := lockfile.Load(t.Context(), lockPath); err != nil {
		t.Fatalf("strict read of regenerated lock: %v", err)
	}
	if info, err := os.Stat(lockPath); err != nil || info.Mode().Perm() != 0o640 {
		t.Fatalf("regenerated lock mode: %v, %v", info, err)
	}
	run(0, "status", "--check")
	for path, content := range before {
		testkit.AssertFileContent(t, filepath.Join(root, path), content)
	}

	legacy := fmt.Sprintf("version = %d\n[[\n", contractversion.LockfileSchema-1)
	if err := os.WriteFile(lockPath, []byte(legacy), 0o640); err != nil {
		t.Fatal(err)
	}
	legacyPreview := requireUnavailableLockComparison(t, run(0, "lock", "--dry-run", "--json"), "legacy", "dry-run")
	if legacyPreview.EntryCounts != preview.EntryCounts {
		t.Fatal("legacy replacement resolved different facts from the same inputs")
	}
	testkit.AssertFileContent(t, lockPath, legacy)
}

func TestLockDescribesUnavailableComparisonsInHumanOutput(t *testing.T) {
	for _, test := range []struct {
		name, preview, written, reason string
		version                        int
	}{
		{"damaged", "would rebuild damaged lockfile:", "rebuilt damaged lockfile:", "contains malformed TOML", contractversion.LockfileSchema},
		{"legacy", "would replace older-schema lockfile:", "replaced older-schema lockfile:", "uses an older schema", contractversion.LockfileSchema - 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			testkit.SetDefaultRootEnv(t, filepath.Join(root, "home"))
			manifest := filepath.Join(root, "daem.toml")
			testkit.WriteFile(t, root, "daem.toml", "version = 1\ntargets = ['codex']\n\n[instructions.project]\nsource = 'source.md'\n")
			testkit.WriteFile(t, root, "source.md", "project instructions\n")
			testkit.WriteFile(t, root, "daem.lock.toml", fmt.Sprintf("version = %d\nold_lock_value = 2026-99-99\n", test.version))

			for _, dryRun := range []bool{true, false} {
				args := []string{"lock", "--manifest", manifest, "--verbose"}
				want := test.written
				if dryRun {
					args = append(args, "--dry-run")
					want = test.preview
				}
				var stdout, stderr bytes.Buffer
				code := testkit.RunCLI(args, &stdout, &stderr)
				text := stdout.String()
				if code != 0 || !strings.HasPrefix(text, want) ||
					!strings.Contains(text, "lock changes not compared:") || !strings.Contains(text, test.reason) ||
					!strings.Contains(text, "lock entries: 2\n") || strings.Contains(text, "lock entry changes:") ||
					strings.Contains(text+stderr.String(), "old_lock_value") ||
					(dryRun && !strings.Contains(text, "locked.subject:")) {
					t.Fatalf("%v: exit %d, stdout %q, stderr %q", args, code, &stdout, &stderr)
				}
			}
		})
	}
}

func requireUnavailableLockComparison(t *testing.T, output, status, mode string) clijson.Lock {
	t.Helper()
	encoded := []byte(output)
	payload := clijson.DecodeLock(t, encoded)
	if payload.Command != "lock" || payload.Mode != mode || payload.PreviousStatus != status || payload.EntryCounts.Subjects != 2 {
		t.Fatalf("unexpected lock result: %s", output)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"has_changes", "change_counts", "order_change_counts", "subject_changes", "order_constraint_changes"} {
		if string(raw[field]) != "null" {
			t.Fatalf("%s = %s, want explicit null", field, raw[field])
		}
	}
	return payload
}
