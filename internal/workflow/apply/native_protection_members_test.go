package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/journal"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/recoverygate"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeProtectionMembersUseExpectedFilesNotNamespacePresence(t *testing.T) {
	for _, test := range []struct {
		name                 string
		previous, retire     bool
		removeFile, protects bool
	}{
		{name: "first creation", protects: true},
		{name: "current keys", previous: true, protects: true},
		{name: "retiring existing file", previous: true, retire: true, protects: true},
		{name: "retiring absent file", previous: true, retire: true, removeFile: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			storage := t.TempDir()
			for name, path := range map[string]string{
				"HOME": "home", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state",
				"XDG_CONFIG_HOME": "config", "PI_CODING_AGENT_DIR": "pi-agent", "CODEX_HOME": "codex",
			} {
				t.Setenv(name, filepath.Join(storage, path))
			}
			root := filepath.Join(storage, "workspace")
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			manifest := filepath.Join(root, "daem.toml")
			if err := os.WriteFile(manifest, []byte("version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\nscope='project'\npath='settings.json'\nsource='./preferences.json'\nownership='keys'\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			fragment := filepath.Join(root, "preferences.json")
			if err := os.WriteFile(fragment, []byte(`{"theme":"fixture-theme"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
				t.Fatal(err)
			}
			if test.previous {
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{}); err != nil {
					t.Fatal("baseline Apply did not produce real native history", err)
				}
			}
			destination := filepath.Join(root, ".pi", "settings.json")
			if test.removeFile {
				if err := os.Remove(destination); err != nil {
					t.Fatal(err)
				}
			}
			if test.retire {
				if err := os.WriteFile(fragment, []byte(`{}`), 0o600); err != nil {
					t.Fatal(err)
				}
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
					t.Fatal(err)
				}
			}
			paths, err := daempaths.Resolve(manifest)
			if err != nil {
				t.Fatal(err)
			}
			barrier, err := recoverygate.NewEffectAuthority(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			planned, err := planReadinessAtPathsWithAssessor(t.Context(), CommandInput{ManifestPath: manifest}, reconcile.ContextApply, paths, &barrier, nil, retainedReadiness)
			if planned.nativeOperation != nil {
				t.Cleanup(func() { _ = planned.nativeOperation.Close() })
			}
			if err != nil {
				t.Fatal(err)
			}
			input, live := planned.nativeOperation.NativeInput()
			if !live || input.Documents == nil {
				t.Fatal("planning did not retain actual native documents")
			}
			positive, err := projectionbuild.BuildPositiveNativePrefix(t.Context(), planned.assessment.Reconciliation.Aggregates())
			if err != nil {
				t.Fatal(err)
			}
			prefix, err := execute.PrepareNativeApplyState(t.Context(), execute.NativeApplyStateInput{
				Before: planned.assessment.CurrentState, Aggregates: positive, StateCodec: planned.nativePlan.stateCodec,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, prospective, err := input.Documents.CaptureMembers(prefix.AfterState(), journal.OperationID(time.Unix(0, 0).UTC()))
			if err != nil {
				t.Fatal(err)
			}
			members, err := nativeProtectionMembers(t.Context(), *input.Documents, prefix, prospective)
			if err != nil || (len(members) == 1) != test.protects || len(members) > 1 {
				t.Fatal("protection membership lost expected file presence", len(members), err)
			}
			if !test.previous || test.removeFile {
				if _, err := os.Stat(destination); !os.IsNotExist(err) {
					t.Fatal("prospective selection created an absent file", err)
				}
			}
			if test.previous && !test.retire && len(prefix.AggregateEffects()) != 0 {
				t.Fatal("current keys manufactured a prefix mutation")
			}
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if members, err := nativeProtectionMembers(ctx, *input.Documents, prefix, prospective); !errors.Is(err, context.Canceled) || len(members) != 0 {
				t.Fatal("cancelled selection returned usable members", err)
			}
			if members, err := nativeProtectionMembers(t.Context(), journal.NativeDocumentObservationBatch{}, prefix, prospective); err == nil || len(members) != 0 {
				t.Fatal("unobserved documents became observed absence")
			}
		})
	}
}
