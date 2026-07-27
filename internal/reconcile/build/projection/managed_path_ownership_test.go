package projection

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestManagedPathOwnershipRelocationTreatsOldAndNewLocalityIndependently(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	owner, err := stateauthority.New(filepath.Join(root, "state.json"), filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, projection := managedPathLock(t, "oracle", "oracle", []target.Target{target.TargetCodex}, "desired")
	projectState := managedPathState(t, projection, []target.Target{target.TargetCodex}, ".agents/skills/oracle", "old")
	globalState, err := durable.NewManagedPathState(
		projectState.Subject(), projectState.ConsumerTargets(), target.ScopeGlobal,
		outputtest.Parse(t, "~/.codex/skills/oracle"), projectState.ContentHash(), projectState.ContentKind(),
		projectState.PermissionPolicy(), projectState.FileMode(),
	)
	if err != nil {
		t.Fatal(err)
	}
	oldGlobal := planningManagedPathOwnershipObservation(
		t, filepath.Join(root, "old-global"), globalState.Destination(), owner, true,
	)
	newGlobal := planningManagedPathOwnershipObservation(
		t, filepath.Join(root, "new-global"), outputtest.Parse(t, "~/.codex/skills/review"), owner, false,
	)

	tests := []struct {
		name         string
		previous     durable.ManagedPathState
		scope        target.Scope
		destination  output.Destination
		observations []observe.OwnershipObservation
		wantKind     reconcile.ManagedPathDecisionKind
		wantReason   reconcile.ActionReason
	}{
		{
			name: "project to global checks only new address", previous: projectState,
			scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/.codex/skills/review"),
			observations: []observe.OwnershipObservation{newGlobal},
			wantKind:     reconcile.ManagedPathReplace, wantReason: reconcile.ReasonContentChanged,
		},
		{
			name: "global to project checks only old address", previous: globalState,
			scope: target.ScopeProject, destination: outputtest.Parse(t, ".agents/skills/review"),
			observations: []observe.OwnershipObservation{oldGlobal},
			wantKind:     reconcile.ManagedPathReplace, wantReason: reconcile.ReasonContentChanged,
		},
		{
			name: "global to global checks both addresses", previous: globalState,
			scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/.codex/skills/review"),
			observations: []observe.OwnershipObservation{oldGlobal, newGlobal},
			wantKind:     reconcile.ManagedPathReplace, wantReason: reconcile.ReasonContentChanged,
		},
		{
			name: "project to global requires new observation", previous: projectState,
			scope: target.ScopeGlobal, destination: outputtest.Parse(t, "~/.codex/skills/review"),
			wantKind: reconcile.ManagedPathBlocked, wantReason: reconcile.ReasonOwnershipObservationMissing,
		},
		{
			name: "global to project requires old active claim", previous: globalState,
			scope: target.ScopeProject, destination: outputtest.Parse(t, ".agents/skills/review"),
			observations: []observe.OwnershipObservation{
				planningManagedPathOwnershipObservation(t, filepath.Join(root, "old-unclaimed"), globalState.Destination(), owner, false),
			},
			wantKind: reconcile.ManagedPathBlocked, wantReason: reconcile.ReasonOwnershipClaimMissing,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			previous := test.previous
			decision := newManagedPathReplace(reconcile.ManagedPathDecisionInput{
				Subject: previous.Subject(), ConsumerTargets: previous.ConsumerTargets(),
				Scope: test.scope, Destination: test.destination,
				DesiredHash: "sha256:new", ContentKind: previous.ContentKind(), Previous: &previous,
			}, reconcile.ReasonContentChanged, "managed destination changed")
			observations, conflicts, err := ownershipObservations(test.observations)
			if err != nil {
				t.Fatal(err)
			}
			got := enforceManagedPathOwnership(decision, true, owner, observations, conflicts)
			if got.Kind() != test.wantKind || got.Reason() != test.wantReason {
				t.Fatalf("decision = %s/%s, want %s/%s", got.Kind(), got.Reason(), test.wantKind, test.wantReason)
			}
		})
	}
}

func TestManagedPathOwnershipForeignClaimOverridesPreliminaryUnmanagedBlock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	requester, err := stateauthority.New(
		filepath.Join(root, "requester-state.json"),
		filepath.Join(root, "requester.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := stateauthority.New(
		filepath.Join(root, "foreign-state.json"),
		filepath.Join(root, "foreign.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(filepath.Join(root, "AGENTS.md"), "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, foreign)
	if err != nil {
		t.Fatal(err)
	}
	claimValue, err := ownership.PresentClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	destination := outputtest.Parse(t, "~/.codex/AGENTS.md")
	observations, conflicts, err := ownershipObservations([]observe.OwnershipObservation{{
		Destination: destination,
		Address:     address,
		Claim:       claimValue,
	}})
	if err != nil {
		t.Fatal(err)
	}
	decision := newManagedPathBlocked(reconcile.ManagedPathDecisionInput{
		Scope: target.ScopeGlobal, Destination: destination,
	}, reconcile.ReasonUnmanagedOutputExists, "destination exists but is not recorded as managed")

	got := enforceManagedPathOwnership(decision, false, requester, observations, conflicts)
	if got.Kind() != reconcile.ManagedPathBlocked || got.Reason() != reconcile.ReasonOwnershipConflict ||
		!strings.Contains(got.Detail(), foreign.ManifestPath()) {
		t.Fatalf("decision = %s/%s %q, want foreign ownership conflict", got.Kind(), got.Reason(), got.Detail())
	}
}

func planningManagedPathOwnershipObservation(
	t *testing.T,
	path string,
	destination output.Destination,
	owner stateauthority.Authority,
	claimed bool,
) observe.OwnershipObservation {
	t.Helper()
	address, err := ownership.NewManagedAddress(path, "")
	if err != nil {
		t.Fatal(err)
	}
	claim := ownership.NoClaim()
	if claimed {
		active, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		claim, _ = ownership.PresentClaim(active)
	}
	return observe.OwnershipObservation{Destination: destination, Address: address, Claim: claim}
}
