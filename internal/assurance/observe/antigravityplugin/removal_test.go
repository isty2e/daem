package antigravityplugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestRemovalObservesExactBundleAbsence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	paths, err := ResolveHostPaths()
	if err != nil {
		t.Fatal(err)
	}
	pending := mustAntigravityPendingRemoval(t, home, "guidance@google")
	assertAntigravityRemovalState(
		t,
		paths,
		pending,
		observepostcondition.EvidenceSatisfied,
	)
	directory, err := paths.PluginDirectoryPath("guidance")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	assertAntigravityRemovalState(
		t,
		paths,
		pending,
		observepostcondition.EvidenceUnsatisfied,
	)
}

func TestRemovalContractRejectsOpaqueSourcesAndWrongRequirements(t *testing.T) {
	pending := mustAntigravityPendingRemoval(t, t.TempDir(), "guidance@google")
	baselines, err := CaptureRemovalBaselines(
		context.Background(),
		pending.Identity().Carrier().Key(),
		pending.EffectPostconditions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(baselines.Baselines()) != 0 {
		t.Fatalf("Antigravity absence baselines = %#v, want none", baselines.Baselines())
	}
	if _, err := CaptureRemovalBaselines(
		context.Background(),
		pending.Identity().Carrier().Key(),
		effectpostcondition.Set{},
	); err == nil {
		t.Fatal("empty Antigravity removal postcondition was accepted")
	}
	if _, err := ObserveRemovalEffects(nil, HostPaths{}, pending); err == nil {
		t.Fatal("nil Antigravity removal context was accepted")
	}
	opaqueSource, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		"./plugins/guidance",
	)
	if err != nil {
		t.Fatal(err)
	}
	opaqueCarrier, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierAntigravityCLIPlugin,
		target.TargetAntigravityCLI,
		target.ScopeGlobal,
		opaqueSource,
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureRemovalBaselines(
		context.Background(),
		opaqueCarrier,
		pending.EffectPostconditions(),
	); err == nil {
		t.Fatal("opaque Antigravity source received removal observation authority")
	}
}

func assertAntigravityRemovalState(
	t *testing.T,
	paths HostPaths,
	pending durablecarrier.PendingCarrierRemoval,
	want observepostcondition.EvidenceState,
) {
	t.Helper()
	evidence, err := ObserveRemovalEffects(context.Background(), paths, pending)
	if err != nil {
		t.Fatal(err)
	}
	facts := evidence.Evidence()
	if evidence.Subject() != pending.Identity().RelationSubject() ||
		!evidence.RouteRequest().Equal(pending.RemoveRequest()) ||
		len(facts) != 1 ||
		facts[0].Requirement() != effectpostcondition.CarrierArtifactsAbsent ||
		facts[0].State() != want {
		t.Fatalf("effect evidence = %#v / %#v, want %q", evidence, facts, want)
	}
}

func mustAntigravityPendingRemoval(
	t *testing.T,
	root string,
	selector string,
) durablecarrier.PendingCarrierRemoval {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		selector,
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    "guidance",
		Carrier: desiredextension.CarrierAntigravityCLIPlugin,
		Target:  target.TargetAntigravityCLI,
		Scope:   target.ScopeGlobal,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := refine.Extensions([]desiredextension.Extension{value})
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(contracts[0])
	if err != nil {
		t.Fatal(err)
	}
	if !admitted {
		t.Fatal("Antigravity extension did not produce managed carrier identity")
	}
	installRequest := mustAntigravityInstallRequest(t, contracts[0])
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
	)
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	realization, ok := contracts[0].Realization()
	if !ok {
		t.Fatal("Antigravity extension has no realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatal("Antigravity extension has no delegated relation")
	}
	removal, admitted, err := lock.ResolveDelegatedCarrierRemoval(
		value.CarrierKey(),
		contracts[0].SubjectID(),
		relation.ExpectedRelation(),
		installRequest,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !admitted {
		t.Fatal("Antigravity selector did not admit removal")
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removal.Request(),
		requirements,
		baselines,
	)
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func mustAntigravityInstallRequest(
	t *testing.T,
	contract lock.LockedSubjectContract,
) realizationdelegate.Request {
	t.Helper()
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
