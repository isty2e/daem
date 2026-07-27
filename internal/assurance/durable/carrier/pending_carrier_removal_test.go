package carrier_test

import (
	"strings"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestPendingCarrierRemovalPreservesClaimAndRemoveRequestIdentity(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "daem.toml"))
	removeRequest := removalRequestForTest(t, "context7")
	effectPostconditions := coupledRemovalPostconditions(t)

	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removeRequest,
		effectPostconditions,
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatalf("NewPendingCarrierRemoval returned error: %v", err)
	}
	if !pending.Claim().ExactEqual(claim) ||
		!pending.Owner().ExactEqual(claim.Owner()) ||
		!pending.Identity().ExactEqual(claim.Identity()) ||
		!pending.RemoveRequest().Equal(removeRequest) ||
		!pending.EffectPostconditions().Equal(effectPostconditions) {
		t.Fatalf("pending removal lost exact identity: %#v", pending)
	}
	if !pending.ExactEqual(pending) {
		t.Fatal("pending removal is not exactly equal to itself")
	}
}

func TestPendingCarrierRemovalOperationIdentityDoesNotDependOnRequestInequality(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "daem.toml"))

	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		claim.InstallRequest(),
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	)
	if err != nil {
		t.Fatalf("same request identity rejected despite distinct removal fact family: %v", err)
	}
	if !pending.RemoveRequest().Equal(claim.InstallRequest()) {
		t.Fatal("same route request identity changed during construction")
	}
}

func TestPendingCarrierRemovalRejectsIncompleteRequest(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "daem.toml"))

	if _, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		realizationdelegate.Request{},
		relationOnlyRemovalPostconditions(),
		durablecarrier.EffectBaselineSet{},
	); err == nil || !strings.Contains(err.Error(), "removal request") {
		t.Fatalf("error = %v, want invalid remove request", err)
	}
}

func TestPendingCarrierRemovalRequiresExactLocalSourceBaseline(t *testing.T) {
	fixture := carrierFixtureFor(t, "context7", "context7@official", target.ScopeProject)
	claim := claimForFixture(t, fixture, mustAuthority(t, t.TempDir(), "daem.toml"))
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.LocalSourceUnchanged},
	)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		requirements,
		durablecarrier.EffectBaselineSet{},
	); err == nil || !strings.Contains(err.Error(), "requires exactly one") {
		t.Fatalf("missing local baseline error = %v", err)
	}

	baseline, err := durablecarrier.NewContentEffectBaseline(
		effectpostcondition.LocalSourceUnchanged,
		artifact.HashFileContent([]byte("before")),
	)
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := durablecarrier.NewEffectBaselineSet([]durablecarrier.EffectBaseline{baseline})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		requirements,
		baselines,
	)
	if err != nil {
		t.Fatalf("local baseline rejected: %v", err)
	}
	stored, present := pending.EffectBaselines().For(effectpostcondition.LocalSourceUnchanged)
	if !present || stored != baseline {
		t.Fatalf("stored baseline = %#v, %t, want %#v", stored, present, baseline)
	}

	withoutRequirement, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		removalRequestForTest(t, "context7"),
		relationOnlyRemovalPostconditions(),
		baselines,
	)
	if err == nil || !strings.Contains(err.Error(), "no matching postcondition") {
		t.Fatalf("orphan baseline error = %v", err)
	}
	_ = withoutRequirement
}

func TestEffectBaselineRejectsUnsupportedOrMalformedContent(t *testing.T) {
	if _, err := durablecarrier.NewAbsentEffectBaseline(
		effectpostcondition.CarrierArtifactsAbsent,
	); err == nil || !strings.Contains(err.Error(), "does not admit") {
		t.Fatalf("unsupported baseline error = %v", err)
	}
	if _, err := durablecarrier.NewContentEffectBaseline(
		effectpostcondition.LocalSourceUnchanged,
		"not-a-hash",
	); err == nil || !strings.Contains(err.Error(), "content hash") {
		t.Fatalf("malformed baseline error = %v", err)
	}
}

func relationOnlyRemovalPostconditions() effectpostcondition.Set {
	return effectpostcondition.Set{}
}

func coupledRemovalPostconditions(t *testing.T) effectpostcondition.Set {
	t.Helper()
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{
			effectpostcondition.CarrierArtifactsAbsent,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return requirements
}

func removalRequestForTest(t *testing.T, key string) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(
		"claude-code.plugin-carrier.remove."+key,
		"v1",
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
