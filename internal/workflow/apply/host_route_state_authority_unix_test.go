//go:build darwin || linux

package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	assurancehostroute "github.com/isty2e/daem/internal/assurance/hostroute"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	executehostroute "github.com/isty2e/daem/internal/effect/execute/hostroute"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

func TestHostRouteRejectsStateDirReplacementDuringCommand(t *testing.T) {
	testHostRouteRejectsStateDirReplacement(t, true)
}

func TestHostRouteRejectsStateDirReplacementBeforePostAttemptWrite(t *testing.T) {
	testHostRouteRejectsStateDirReplacement(t, false)
}

func testHostRouteRejectsStateDirReplacement(t *testing.T, replaceDuringCommand bool) {
	t.Helper()
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	stateDir := filepath.Join(root, ".daem")
	movedStateDir := filepath.Join(root, ".daem-moved")
	statePath := filepath.Join(stateDir, "state.json")
	writeApplyFile(t, manifestPath, "version = 1\ntargets = [\"claude-code\"]\n")

	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetClaudeCode},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Extensions: []extension.Extension{
			writeAheadClaudeExtension(t, "project-plugin", "project@market", target.ScopeProject),
		},
	})
	locked, err := lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatalf("BuildWithOptions returned error: %v", err)
	}
	writeApplyLockfile(t, lockfilePath, locked)

	missing := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	missingObservations := applyClaudeObservationBatchForLocked(t, locked, missing)
	replaceStateDir := func() error {
		if err := os.Rename(stateDir, movedStateDir); err != nil {
			return err
		}
		return os.Mkdir(stateDir, 0o700)
	}
	runnerCalled := false
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			runnerCalled = true
			if replaceDuringCommand {
				if err := replaceStateDir(); err != nil {
					return subprocess.CommandResult{Started: true, Err: err}
				}
			}
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
		},
	})
	var observer HostRouteObserver
	if !replaceDuringCommand {
		observer = func(
			context.Context,
			executehostroute.Command,
			[]durablecarrier.PendingCarrierInstall,
			[]durablecarrier.ManagedCarrierClaim,
		) assurancehostroute.ObservationFact {
			if err := replaceStateDir(); err != nil {
				t.Fatalf("replace StateDir during observation: %v", err)
			}
			return assurancehostroute.ObservationUnavailable(
				assurancehostroute.ResultReasonObservationUnavailable,
			)
		}
	}

	planned, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:         manifestPath,
		LockfilePath:         lockfilePath,
		TargetValues:         []string{"claude-code"},
		RelationObservations: &missingObservations,
	})
	if err != nil {
		t.Fatalf("PlanWrite returned error: %v", err)
	}
	_, err = ExecuteWithOptions(t.Context(), planned, ExecuteOptions{
		RelationObservations: &missingObservations,
		HostRouteExecutor:    executor,
		HostRouteObserver:    observer,
		PlanWasDisclosed:     true,
	})
	if !runnerCalled {
		t.Fatalf("host route runner was not called: %v", err)
	}
	if !hasRootedPathFailureKind(err, rootedpath.FailureRootReplaced) ||
		!errors.Is(err, fileset.ErrFileSetFenceUnprovable) {
		t.Fatalf("ExecuteWithOptions error = %v, want rooted replacement and file-set fence failure", err)
	}
	if _, statErr := os.Stat(statePath); !os.IsNotExist(statErr) {
		t.Fatalf("replacement statefile %q stat error = %v, want absent", statePath, statErr)
	}
	movedStatePath := filepath.Join(movedStateDir, "state.json")
	content, readErr := os.ReadFile(movedStatePath)
	if readErr != nil {
		t.Fatalf("read moved statefile: %v", readErr)
	}
	snapshot, decodeErr := (statefile.Codec{}).Decode(content)
	if decodeErr != nil {
		t.Fatalf("decode moved statefile: %v", decodeErr)
	}
	if len(snapshot.HostRouteAttempts()) != 0 || len(snapshot.ManagedCarrierClaims()) != 0 {
		t.Fatalf("post-attempt facts persisted through replaced StateDir: %#v", snapshot)
	}
}
