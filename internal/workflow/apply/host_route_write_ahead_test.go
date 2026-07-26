package apply

import (
	"context"
	"path/filepath"
	"testing"

	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestHostRouteWriteAheadCarrierFactIsScopedToCurrentInvocation(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	statePath := filepath.Join(root, ".daem", "state.json")
	writeApplyFile(t, manifestPath, "version = 1\ntargets = [\"claude-code\"]\n")

	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetClaudeCode},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Extensions: []extension.Extension{
			writeAheadClaudeExtension(t, "project-plugin", "project@market", target.ScopeProject),
			writeAheadClaudeExtension(t, "global-plugin", "global@market", target.ScopeGlobal),
		},
	})
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	writeApplyLockfile(t, lockfilePath, locked)

	missing := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	missingObservations := applyClaudeObservationBatchForLocked(t, locked, missing)
	invocations := 0
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			invocations++
			state := loadApplyStatefile(t, statePath)
			pending := state.PendingCarrierInstalls()
			if len(pending) != 1 {
				t.Fatalf("invocation %d saw pending facts %#v, want only current write-ahead row", invocations, pending)
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})

	planned, err := PlanWrite(context.Background(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &missingObservations,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	if _, err := ExecuteWithOptions(context.Background(), planned, ExecuteOptions{
		RelationObservations: &missingObservations,
		HostRouteExecutor:    executor,
	}); err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	if invocations != 2 {
		t.Fatalf("host route invocations = %d, want 2", invocations)
	}
}

func writeAheadClaudeExtension(t *testing.T, id string, selector string, scope target.Scope) extension.Extension {
	t.Helper()
	return desiredtest.Extension(t, extension.Spec{
		Name:    id,
		Carrier: extension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, extension.SourceKindMarketplace, selector),
	})
}
