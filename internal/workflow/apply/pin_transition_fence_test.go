package apply

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/refresh"
)

func (fixture piPinApplyFixture) reserve(t *testing.T) durablecarrier.PinTransition {
	t.Helper()
	pair, err := fixture.claim.PinTransitionTo(fixture.locked.Locked.Subjects()[0])
	if err != nil {
		t.Fatal(err)
	}
	if fixture.scope == target.ScopeProject {
		path := filepath.Join(fixture.root, ".daem", "state.json")
		state := loadApplyStatefile(t, path)
		next, err := state.WithReservedCarrierPinTransition(pair)
		if err != nil {
			t.Fatal(err)
		}
		writeApplyStatefile(t, path, next)
	} else {
		store, err := carrierclaimstore.New(isolatedApplyCarrierRegistryPath(t, fixture.root))
		if err != nil {
			t.Fatal(err)
		}
		registry, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReservePinTransitionIfCurrent(t.Context(), registry, pair); err != nil {
			t.Fatal(err)
		}
	}
	return pair
}

func TestPiPinTransitionRefusesStalePreparedReservation(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, scope)
			planning, err := PlanWrite(t.Context(), fixture.input())
			if err != nil {
				t.Fatal(err)
			}
			fixture.reserve(t)
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Clock: fixedApplyHostRouteClock, Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			if _, err := ExecuteWithOptions(t.Context(), planning, ExecuteOptions{HostRouteExecutor: executor}); err == nil {
				t.Fatal("stale pre-reservation approval was accepted")
			}
			if calls != 0 {
				t.Fatalf("stale plan invoked host %d times", calls)
			}
			claims := loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
			if len(claims) != 1 {
				t.Fatalf("retained claims=%d", len(claims))
			}
			if _, pending := claims[0].PendingPinTransition(); !pending {
				t.Fatal("stale execution erased reservation")
			}
		})
	}
}

func TestPiPinTransitionRefusesDesiredAndKnownSharedConsumers(t *testing.T) {
	for _, scenario := range []string{"desired_unclaimed", "foreign_global", "observed_alias"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newPiPinApplyFixture(t, target.ScopeGlobal)
			switch scenario {
			case "desired_unclaimed":
				content, err := os.ReadFile(fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, fixture.manifest, string(content)+fmt.Sprintf("\n[[extension]]\nid = \"second\"\ncarrier = \"pi-package\"\ntargets = [\"pi\"]\nscope = \"global\"\nsource = { host_source = %q }\n", fixture.before))
				environment, err := declarationmanifest.Load(t.Context(), fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				_, err = lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{ExtensionOrderIdentity: aggregatecodec.ExtensionOrderIdentityResolver(applyTestPaths(t, fixture.root))})
				if err == nil || !strings.Contains(err.Error(), "host load identity") || !strings.Contains(err.Error(), "appears more than once") {
					t.Fatalf("duplicate desired native identity reached apply: %v", err)
				}
				return
			case "foreign_global":
				other := filepath.Join(fixture.root, "other")
				manifest := filepath.Join(other, "daem.toml")
				writeApplyFile(t, manifest, "version = 1\ntargets = [\"pi\"]\n")
				seedApplyCarrierClaim(t, other, manifest, piPinApplyLock(t, target.ScopeGlobal, fixture.before), target.ScopeGlobal)
			case "observed_alias":
				alias := strings.Replace(fixture.before, "git:github.com/", "git:https://github.com/", 1)
				writeApplyFile(t, fixture.settings, fmt.Sprintf("{\"packages\":[%q,%q]}", fixture.before, alias))
			}
			if _, err := PlanWrite(t.Context(), fixture.input()); err == nil || !strings.Contains(err.Error(), "pin_transition_conflict") {
				t.Fatalf("shared-consumer admission = %v", err)
			}
		})
	}
}

func TestPiPinReservationRefusesRemovalAfterDeclarationDisappears(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, settingsPresent := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/settings_%t", scope, settingsPresent), func(t *testing.T) {
				fixture := newPiPinApplyFixture(t, scope)
				fixture.reserve(t)
				writeApplyFile(t, fixture.manifest, "version = 1\ntargets = [\"pi\"]\n")
				environment, err := declarationmanifest.Load(t.Context(), fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				locked, err := lockbuild.BuildWithOptions(t.Context(), environment, nil, lockbuild.Options{})
				if err != nil {
					t.Fatal(err)
				}
				writeApplyLockfile(t, fixture.lockfile, locked)
				if !settingsPresent {
					writeApplyFile(t, fixture.settings, "{\"packages\":[]}")
				}

				planned, err := PlanWrite(t.Context(), fixture.input())
				if planned != nil {
					defer planned.Close()
				}
				if err == nil {
					t.Fatal("pending native transition admitted removal or already-absent retirement")
				}
				claims := loadCarrierClaimsForScope(t, fixture.root, fixture.manifest, scope)
				if len(claims) != 1 {
					t.Fatal("removal preview changed management")
				}
				if _, pending := claims[0].PendingPinTransition(); !pending {
					t.Fatal("removal preview erased native uncertainty")
				}
			})
		}
	}
}

func TestPiPinReservationFencesRefreshAcrossScopes(t *testing.T) {
	for _, pendingScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, refreshScope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
			t.Run(string(pendingScope)+"/"+string(refreshScope), func(t *testing.T) {
				fixture := newPiPinApplyFixture(t, pendingScope)
				fixture.reserve(t)
				writeApplyFile(t, fixture.manifest, fmt.Sprintf("version = 1\ntargets = [\"pi\"]\n[[extension]]\nid = \"tools-managed\"\ncarrier = \"pi-package\"\ntargets = [\"pi\"]\nscope = %q\nsource = { host_source = %q }\n", refreshScope, fixture.after))
				writeApplyLockfile(t, fixture.lockfile, piPinApplyLock(t, refreshScope, fixture.after))
				result, err := refresh.PlanDryRun(t.Context(), refresh.CommandInput{ManifestPath: fixture.manifest, ExtensionID: "tools-managed"}, refresh.PlanOptions{})
				if err == nil || result.ReasonCode != refresh.ReasonMutationAuthority || !strings.Contains(err.Error(), "pending pin transition") {
					t.Fatalf("refresh fence = %s, %v", result.ReasonCode, err)
				}

				unrelated := "git:github.com/example/unrelated@" + strings.Repeat("c", 40)
				content, err := os.ReadFile(fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				writeApplyFile(t, fixture.manifest, strings.Replace(string(content), fixture.after, unrelated, 1))
				writeApplyLockfile(t, fixture.lockfile, piPinApplyLock(t, refreshScope, unrelated))
				if _, err := refresh.PlanDryRun(t.Context(), refresh.CommandInput{ManifestPath: fixture.manifest, ExtensionID: "tools-managed"}, refresh.PlanOptions{}); err != nil {
					t.Fatalf("unrelated refresh was fenced: %v", err)
				}
			})
		}
	}
}
