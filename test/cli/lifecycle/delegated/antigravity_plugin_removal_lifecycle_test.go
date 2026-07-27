package cli_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestAntigravityPluginDesiredAbsenceRemovesExactManagedHostPair(t *testing.T) {
	fixture := newAntigravityRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.assertClaimCount(t, 1)
	fixture.removeDeclaration(t)
	fixture.runApply(t)

	if len(fixture.requests) != 2 {
		t.Fatalf("lifecycle requests = %#v, want install and uninstall", fixture.requests)
	}
	fixture.assertRequest(t, fixture.requests[1], "uninstall")
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertRemovalBoundaries(t)

	fixture.runApply(t)
	if len(fixture.requests) != 2 {
		t.Fatalf("converged retry requests = %#v, want no reinvocation", fixture.requests)
	}
}

func TestAntigravityPluginSuccessfulExitWithoutEffectsRetainsPendingRemoval(t *testing.T) {
	fixture := newAntigravityRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.removeDeclaration(t)

	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			fixture.assertRequest(t, request, "uninstall")
			return subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    0,
				Stdout:      "Uninstalled plugin successfully",
			}
		},
	)
	if exitCode != 1 ||
		stderr != "" ||
		(!strings.Contains(stdout, "observed") &&
			!strings.Contains(stdout, "postcondition")) {
		t.Fatalf("false-success apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	fixture.assertClaimCount(t, 1)
	fixture.assertPendingRemovalCount(t, 1)
	fixture.assertSelectedPresent(t)

	fixture.runApply(t)
	if len(fixture.requests) != 3 {
		t.Fatalf("retry requests = %#v, want exact uninstall reinvocation", fixture.requests)
	}
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertRemovalBoundaries(t)
}

func TestAntigravityPluginPartialRemovalFailsClosedAndRetries(t *testing.T) {
	fixture := newAntigravityRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.removeDeclaration(t)

	exitCode, _, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			fixture.assertRequest(t, request, "uninstall")
			if err := os.Remove(
				filepath.Join(fixture.selectedPluginRoot, "plugin.json"),
			); err != nil {
				t.Fatal(err)
			}
			return subprocess.CommandResult{
				Started:     true,
				HasExitCode: true,
				ExitCode:    0,
			}
		},
	)
	if exitCode != 1 || stderr != "" {
		t.Fatalf("partial apply exitCode=%d stderr=%q", exitCode, stderr)
	}
	fixture.assertClaimCount(t, 1)
	fixture.assertPendingRemovalCount(t, 1)

	fixture.runApply(t)
	if len(fixture.requests) != 3 {
		t.Fatalf("partial retry requests = %#v, want uninstall retry", fixture.requests)
	}
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertRemovalBoundaries(t)
}

func TestAntigravityPluginTimedOutCommandConvergesOnlyAfterObservedAbsence(
	t *testing.T,
) {
	fixture := newAntigravityRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.removeDeclaration(t)

	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
			fixture.assertRequest(t, request, "uninstall")
			fixture.writeHostState(t, false)
			return subprocess.CommandResult{
				Started:  true,
				TimedOut: true,
				Err:      context.DeadlineExceeded,
			}
		},
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("observed timeout convergence exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertRemovalBoundaries(t)
}

func TestAntigravityPluginAlreadyAbsentRetiresWithoutHostInvocation(t *testing.T) {
	fixture := newAntigravityRemovalLifecycleFixture(t)
	fixture.install(t)
	fixture.removeDeclaration(t)
	fixture.writeHostState(t, false)

	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			t.Fatal("already-absent Antigravity relation invoked host uninstall")
			return subprocess.CommandResult{}
		},
	)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("already-absent apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if len(fixture.requests) != 1 {
		t.Fatalf("already-absent requests = %#v, want install only", fixture.requests)
	}
	fixture.assertClaimCount(t, 0)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertRemovalBoundaries(t)
}

func TestAntigravityGlobalRemovalBlocksForAnotherDaemKnownConsumer(t *testing.T) {
	fixture := newAntigravityRemovalLifecycleFixture(t)
	fixture.install(t)
	claims := loadCLIGlobalCarrierClaims(t, fixture.manifestPath)
	if len(claims) != 1 {
		t.Fatalf("initial global claims = %#v, want one", claims)
	}
	otherRoot := filepath.Join(fixture.root, "other-project")
	owner, err := stateauthority.New(
		filepath.Join(otherRoot, ".daem", "state.json"),
		filepath.Join(otherRoot, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	sharedClaim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		claims[0].Identity(),
		claims[0].InstallRequest(),
		claims[0].Provenance(),
	)
	if err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(fixture.manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	store, err := carrierclaimstore.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(t.Context(), sharedClaim); err != nil {
		t.Fatal(err)
	}
	fixture.removeDeclaration(t)

	exitCode, stdout, stderr := fixture.runApplyWithRunner(
		t,
		func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
			t.Fatal("shared Antigravity carrier invoked host uninstall")
			return subprocess.CommandResult{}
		},
	)
	if exitCode != 1 ||
		stderr != "" ||
		!strings.Contains(stdout, "remaining_daem_known_consumers") {
		t.Fatalf("shared-carrier apply exitCode=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if len(fixture.requests) != 1 {
		t.Fatalf("shared-carrier requests = %#v, want install only", fixture.requests)
	}
	fixture.assertClaimCount(t, 2)
	fixture.assertPendingRemovalCount(t, 0)
	fixture.assertSelectedPresent(t)
}
