package cli_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/clijson"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

func TestRecoverCleanupRefusal(t *testing.T) {
	t.Run("ordinary", func(t *testing.T) {
		manifest := filepath.Join(t.TempDir(), "daem.toml")
		paths, state, _, _ := captureCLIRecoveryDeleteJournal(t, manifest)
		testkit.WriteStatefile(t, paths.StatefilePath, state)
		checkCleanupRefusals(t, manifest, paths.OwnershipRegistryPath, "clean_before", "clean_before", []string{"cleanup"})
	})
	t.Run("mixed native rollback", func(t *testing.T) {
		f := interruptMixedCreate(t)
		checkCleanupRefusals(t, f.Manifest, f.Paths.OwnershipRegistryPath, "needs_rollback", "blocked", []string{"restore_delete", "restore_delete"})
	})
}

func checkCleanupRefusals(t *testing.T, manifest, registry, assessed, hardRefusal string, assessedActions []string) {
	t.Helper()
	workspace := filepath.Dir(manifest)
	residue, cleanup := instructionRecoverySlots(t, manifest)
	run := func(t *testing.T, mode string, wantExit int) clijson.Recover {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := testkit.RunVerboseCLI([]string{"recover", "--manifest", manifest, mode, "--json"}, &stdout, &stderr)
		if code != wantExit {
			t.Fatalf("%s exit=%d, want %d: %s %s", mode, code, wantExit, &stdout, &stderr)
		}
		return clijson.DecodeRecover(t, stdout.Bytes())
	}
	baseline := run(t, "--dry-run", 0)
	var kinds []string
	for _, action := range baseline.Actions {
		kinds = append(kinds, action.Kind)
	}
	if baseline.Classification != assessed || baseline.HasErrors || !slices.Equal(kinds, assessedActions) {
		t.Fatalf("undisturbed plan: %#v", baseline)
	}

	check := func(t *testing.T, classification, readiness, reason string) {
		t.Helper()
		for _, mode := range []string{"--dry-run", "--yes"} {
			result := run(t, mode, 1)
			if !result.HasErrors || result.Phase != "planned" || result.Classification != classification {
				t.Fatalf("unexpected %s refusal: %#v", mode, result)
			}
			if classification == "blocked" {
				if len(result.Actions) != 1 || result.Actions[0].Kind != "error" {
					t.Fatalf("hard refusal advertised restoration: %#v", result.Actions)
				}
			} else if !reflect.DeepEqual(result.Actions, baseline.Actions) {
				t.Fatalf("%s changed the assessed actions: %#v", reason, result.Actions)
			}
			found := false
			for _, row := range result.CleanupObligations {
				if row.Destination == "AGENTS.md" && row.Readiness == readiness && row.Reason == reason {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s reason: %#v", reason, result.CleanupObligations)
			}
		}
	}
	testkit.WriteFile(t, workspace, filepath.Base(residue), "leftover")
	snapshot := func(t *testing.T) map[string]nativefixture.RecoveryImage {
		return nativefixture.SnapshotRecoveryPaths(t, workspace, registry)
	}
	t.Run("unreadable", func(t *testing.T) {
		before := snapshot(t)
		if err := os.Chmod(residue, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(residue, before[residue].Mode.Perm()); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, snapshot(t)) {
				t.Error("retry changed the workspace or registry")
			}
		})
		if _, err := os.ReadFile(residue); err == nil {
			t.Skip("fixture user can read mode 000 files")
		} else if !errors.Is(err, os.ErrPermission) {
			t.Fatal(err)
		}
		check(t, assessed, "retry", "residue_unavailable")
		if info, err := os.Stat(residue); err != nil || info.Mode().Perm() != 0 {
			t.Fatal("unreadable residue changed", err)
		}
	})
	t.Run("collision", func(t *testing.T) {
		testkit.WriteFile(t, workspace, filepath.Base(cleanup), "staged")
		before := snapshot(t)
		check(t, hardRefusal, "blocked", "cleanup_stage_collision")
		if !reflect.DeepEqual(before, snapshot(t)) {
			t.Fatal("hard refusal changed the workspace or registry")
		}
	})
}

func interruptMixedCreate(t *testing.T) nativefixture.RecoveryFixture {
	t.Helper()
	f := nativefixture.NewRecoveryFixture(t, "absent")
	root := filepath.Dir(f.Manifest)
	testkit.WriteFile(t, root, "daem.toml", string(testkit.ReadFile(t, f.Manifest))+"\n[instructions.project]\nsource='./instructions.md'\n")
	testkit.WriteFile(t, root, "instructions.md", "instructions\n")
	nativefixture.RunRecoveryCommand(t, f, "none", "lock")
	watched := f
	watched.Destination = filepath.Join(root, "AGENTS.md")
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, watched, "host", "apply", "--yes")
	if code != nativefixture.RecoveryExit {
		t.Fatalf("mixed create did not stop at host publication: %d %s %s", code, stdout, stderr)
	}
	return f
}

func instructionRecoverySlots(t *testing.T, manifest string) (string, string) {
	t.Helper()
	prepared, err := recoverworkflow.Plan(t.Context(), recoverworkflow.PlanInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	}()
	var obligations []recovery.RemovalCleanupObligation
	selection := prepared.Disclosure()
	if plan, ok := journal.ActiveRecoveryPlan(selection); ok {
		obligations = plan.RemovalCleanupObligations()
	} else if plan, ok := journal.NativeActiveRecoveryPlan(selection); ok {
		obligations = plan.RemovalCleanupObligations()
	}
	for _, obligation := range obligations {
		if obligation.Destination().String() == "AGENTS.md" {
			names := obligation.Names()
			return filepath.Join(filepath.Dir(manifest), names.Residue()), filepath.Join(filepath.Dir(manifest), names.Cleanup())
		}
	}
	t.Fatal("no removal intent for project instructions")
	return "", ""
}
