package apply

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/desired/extension"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

type piPinApplyFixture struct {
	root, manifest, lockfile, settings string
	before, after                      string
	scope                              target.Scope
	claim                              durablecarrier.ManagedCarrierClaim
	locked                             lock.File
}

func newPiPinApplyFixture(t *testing.T, scope target.Scope) piPinApplyFixture {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-agent"))
	fixture := piPinApplyFixture{
		root: root, manifest: filepath.Join(root, "daem.toml"), lockfile: filepath.Join(root, "daem.lock.toml"), scope: scope,
		before: "git:github.com/example/package@" + strings.Repeat("a", 40),
		after:  "git:github.com/example/package@" + strings.Repeat("b", 40),
	}
	writeApplyFile(t, fixture.manifest, fmt.Sprintf("version = 1\ntargets = [\"pi\"]\n[[extension]]\nid = \"tools-managed\"\ncarrier = \"pi-package\"\ntargets = [\"pi\"]\nscope = %q\nsource = { host_source = %q }\n", scope, fixture.after))
	old := piPinApplyLock(t, scope, fixture.before)
	fixture.locked = piPinApplyLock(t, scope, fixture.after)
	writeApplyLockfile(t, fixture.lockfile, fixture.locked)
	fixture.claim = seedApplyCarrierClaimWithProvenance(t, root, fixture.manifest, old, scope, durablecarrier.ClaimProvenanceExplicitlyAdoptedObserved)
	var err error
	fixture.settings, err = observepipackage.SettingsPath(observepipackage.SettingsInput{WorkDir: root, ProjectRoot: root, Scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.before))
	return fixture
}

func piPinApplyLock(t *testing.T, scope target.Scope, source string) lock.File {
	t.Helper()
	environment := applyCarrierEnvironment(t, "tools-managed", extension.CarrierPiPackage, target.TargetPi, scope, extension.SourceKindHostSource, source)
	file, err := lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func (fixture piPinApplyFixture) input() CommandInput {
	return CommandInput{ManifestPath: fixture.manifest, LockfilePath: fixture.lockfile, TargetValues: []string{"pi"}}
}

func TestApplyPiPinTransitionWritesIntentBeforeNativeAttempt(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, scope)
			planning, err := PlanWrite(t.Context(), fixture.input())
			if err != nil {
				t.Fatal(err)
			}
			actions := planning.Reconciliation.Relations()
			if len(actions) != 1 || actions[0].Kind() != reconcile.ActionChangePin {
				t.Fatalf("relations = %#v", actions)
			}
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Clock: fixedApplyHostRouteClock, Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				claims := loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
				if len(claims) != 1 {
					t.Fatalf("claims before native attempt = %d", len(claims))
				}
				pair, present := claims[0].PendingPinTransition()
				if !present || !pair.Before().ExactEqual(fixture.claim) || pair.Identity().Carrier().Key().Source().Ref() != fixture.after {
					t.Fatal("native attempt ran without exact durable intent and old management")
				}
				want := []string{"install", fixture.after}
				if scope == target.ScopeProject {
					want = append(want, "-l")
				}
				if request.Command != "pi" || !slices.Equal(request.Args, want) {
					t.Fatalf("native invocation = %s %v", request.Command, request.Args)
				}
				writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q]}", fixture.after))
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			result, err := ExecuteWithOptions(t.Context(), planning, ExecuteOptions{HostRouteExecutor: executor})
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || len(result.HostRouteAttempts) != 1 {
				t.Fatalf("calls/records = %d/%d", calls, len(result.HostRouteAttempts))
			}
			claims := loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
			if len(claims) != 1 || !claims[0].MatchesLockedRecord(fixture.locked.Locked.Subjects()[0]) || claims[0].Provenance() != durablecarrier.ClaimProvenancePinTransitionObserved {
				t.Fatal("successful exact native observation did not replace management")
			}
			repeat, err := PlanWrite(t.Context(), fixture.input())
			if err != nil {
				t.Fatal(err)
			}
			if repeat.Reconciliation.Relations()[0].InvokesHostRoute() {
				t.Fatal("settled pin was scheduled again")
			}
		})
	}
}
