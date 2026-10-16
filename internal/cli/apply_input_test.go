package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/reconcile"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func TestRunApplyInputDiagnostics(t *testing.T) {
	for _, input := range []struct {
		name     string
		manifest string
		lock     string
		missing  string
	}{
		{name: "unknown key", manifest: "version=1\ntargets=[\"codex\"]\nbogus=true\n"},
		{name: "TOML position", manifest: "version=1\ntargets=[\n"},
		{name: "required transport", manifest: "version=1\ntargets=[\"claude-code\"]\n[[mcp_server]]\nname=\"broken\"\ncommand=\"must-not-run\"\n"},
		{name: "multiple MCP targets", manifest: "version=1\ntargets=[\"codex\",\"claude-code\"]\n[[mcp_server]]\nname=\"broken\"\ntransport=\"stdio\"\ncommand=\"must-not-run\"\ntargets=[\"codex\",\"claude-code\"]\n"},
		{name: "missing manifest", missing: "manifest"},
		{name: "missing default manifest", missing: "default manifest"},
		{name: "missing lock", missing: "lock"},
		{name: "malformed lock", lock: "\nsubjects=[\n"},
	} {
		for _, mode := range []struct {
			name string
			args []string
		}{
			{name: "dry-run", args: []string{"--dry-run"}},
			{name: "yes", args: []string{"--yes"}},
			{name: "dry-run JSON", args: []string{"--dry-run", "--json"}},
			{name: "yes JSON", args: []string{"--yes", "--json"}},
		} {
			t.Run(input.name+"/"+mode.name, func(t *testing.T) {
				root := isolatedApplyAuthorityRoot(t)
				t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
				project := filepath.Join(root, "project with spaces")
				if err := os.Mkdir(project, 0o700); err != nil {
					t.Fatal(err)
				}
				t.Chdir(project)
				manifestPath := filepath.Join(project, "daem.toml")
				lockPath := filepath.Join(project, "daem.lock.toml")
				if err := os.WriteFile(manifestPath, []byte("version=1\ntargets=[\"codex\"]\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				var setupOut, setupErr bytes.Buffer
				if code := runWithoutTerminal([]string{"lock"}, &setupOut, &setupErr); code != 0 {
					t.Fatalf("seed lock code=%d stdout=%q stderr=%q", code, &setupOut, &setupErr)
				}

				if input.manifest != "" {
					if err := os.WriteFile(manifestPath, []byte(input.manifest), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				var validLock []byte
				if input.lock != "" {
					original, err := os.ReadFile(lockPath)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(lockPath, append(original, input.lock...), 0o600); err != nil {
						t.Fatal(err)
					}
					validLock = original
				}
				if input.missing != "" {
					missingPath := manifestPath
					if input.missing == "lock" {
						missingPath = lockPath
					}
					if err := os.Remove(missingPath); err != nil {
						t.Fatal(err)
					}
				}

				var inputErr error
				if input.lock != "" || input.missing == "lock" {
					_, inputErr = lockfile.Load(context.Background(), lockPath)
					if inputErr == nil {
						t.Fatal("lock fixture did not produce an input failure")
					}
					inputErr = fmt.Errorf("%w: %w", applyworkflow.ErrReadLockfile, inputErr)
				} else {
					selection := manifestPath
					if input.missing == "default manifest" {
						selection = ""
					}
					paths, err := daempaths.Resolve(selection)
					if err != nil {
						t.Fatal(err)
					}
					_, inputErr = declarationmanifest.LoadSelected(context.Background(), paths)
					if inputErr == nil {
						t.Fatal("manifest fixture did not produce an input failure")
					}
					inputErr = fmt.Errorf("invalid manifest: %w", inputErr)
				}
				expected := clipresent.Error(inputErr)
				before := snapshotApplyInputFiles(t, root)
				var stdout, stderr bytes.Buffer
				args := append([]string{"apply"}, mode.args...)
				if input.missing == "manifest" {
					args = append(args, "--manifest", manifestPath)
				}
				if code := runWithoutTerminal(args, &stdout, &stderr); code != 1 || stdout.Len() != 0 {
					t.Errorf("refusal code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
				}
				if !strings.Contains(stderr.String(), expected) {
					t.Errorf("input diagnostic differs from shared producer: stderr=%q expected=%q", &stderr, expected)
				}
				var unavailablePlan bytes.Buffer
				clipresent.PrintActionPlanWithOptions(&unavailablePlan, "apply", reconcile.Result{}, clipresent.HumanOptions{})
				if strings.Contains(stderr.String(), unavailablePlan.String()) {
					t.Errorf("printed a reconciliation that was not produced: %q", &stderr)
				}
				if strings.Contains(stderr.String(), "<manifest>") {
					t.Errorf("unusable manifest placeholder: %q", &stderr)
				}

				var lockHint bytes.Buffer
				printLockCommandHint(&lockHint, manifestPath, "")
				if input.missing == "lock" {
					if !strings.Contains(stderr.String(), lockHint.String()) {
						t.Errorf("missing executable lock hint: stderr=%q expected=%q", &stderr, &lockHint)
					}
				} else if input.lock != "" && strings.Contains(stderr.String(), lockHint.String()) {
					t.Errorf("corrupt lock recommended a command that rereads it: %q", &stderr)
				}
				if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
					t.Errorf("refused input changed files: before=%v after=%v", before, after)
				}
				if input.lock != "" {
					stdout.Reset()
					stderr.Reset()
					if code := runWithoutTerminal([]string{"lock", "--dry-run", "--manifest", manifestPath}, &stdout, &stderr); code == 0 {
						t.Fatal("lock creation unexpectedly repaired malformed input")
					}
					if err := os.WriteFile(lockPath, validLock, 0o600); err != nil {
						t.Fatal(err)
					}
					stdout.Reset()
					stderr.Reset()
					if code := runWithoutTerminal([]string{"apply", "--dry-run"}, &stdout, &stderr); code != 0 {
						t.Fatalf("syntax correction did not restore planning: code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
					}
				}
				if input.missing == "lock" {
					stdout.Reset()
					stderr.Reset()
					if code := runWithoutTerminal([]string{"lock", "--manifest", manifestPath}, &stdout, &stderr); code != 0 {
						t.Fatalf("proposed correction failed: code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
					}
					stdout.Reset()
					stderr.Reset()
					if code := runWithoutTerminal([]string{"apply", "--dry-run"}, &stdout, &stderr); code != 0 {
						t.Fatalf("corrected input still failed: code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
					}
					plan, err := applyworkflow.PlanDryRun(context.Background(), applyworkflow.CommandInput{ManifestPath: manifestPath})
					if err != nil || !plan.ReconciliationReady || plan.Reconciliation.ProjectionDecisionCount() != 0 {
						t.Fatalf("real empty-plan control failed: ready=%t err=%v", plan.ReconciliationReady, err)
					}
					var expectedPlan bytes.Buffer
					clipresent.PrintDryRunPlanWithOptions(&expectedPlan, plan.Reconciliation, clipresent.HumanOptions{})
					if !strings.Contains(stdout.String(), expectedPlan.String()) {
						t.Errorf("real empty plan was suppressed: stdout=%q expected=%q", &stdout, &expectedPlan)
					}
				}
			})
		}
	}
}

func TestRunApplyExistingNonregularLockIsNotTreatedAsAbsent(t *testing.T) {
	for _, mode := range [][]string{{"--dry-run"}, {"--yes"}, {"--dry-run", "--json"}, {"--yes", "--json"}} {
		t.Run(strings.Join(mode, " "), func(t *testing.T) {
			root := isolatedApplyAuthorityRoot(t)
			t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
			t.Chdir(root)
			manifestPath := filepath.Join(root, "daem.toml")
			lockPath := filepath.Join(root, "daem.lock.toml")
			if err := os.WriteFile(manifestPath, []byte("version=1\ntargets=[\"codex\"]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(lockPath, 0o700); err != nil {
				t.Fatal(err)
			}
			before := snapshotApplyInputFiles(t, root)
			var stdout, stderr, lockHint bytes.Buffer
			printLockCommandHint(&lockHint, manifestPath, "")
			if code := runWithoutTerminal(append([]string{"apply"}, mode...), &stdout, &stderr); code != 1 || stdout.Len() != 0 {
				t.Fatalf("existing bad lock refusal: code=%d stdout=%q stderr=%q", code, &stdout, &stderr)
			}
			if !strings.Contains(stderr.String(), clipresent.Escape(lockPath)) || strings.Contains(stderr.String(), lockHint.String()) {
				t.Errorf("lost existing-file cause or suggested creation: %q", &stderr)
			}
			if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Errorf("unreadable input changed files: before=%v after=%v", before, after)
			}
		})
	}
}

type applyInputFileSnapshot struct {
	mode   fs.FileMode
	digest [sha256.Size]byte
}

func snapshotApplyInputFiles(t *testing.T, root string) map[string]applyInputFileSnapshot {
	t.Helper()
	result := make(map[string]applyInputFileSnapshot)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		item := applyInputFileSnapshot{mode: info.Mode()}
		if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			item.digest = sha256.Sum256(content)
		}
		result[path] = item
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
