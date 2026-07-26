package execute

import (
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func testManagedCarrierIdentity(
	t *testing.T,
	subject topology.SubjectID,
	subjectKey hostrelation.SubjectKey,
) (durablecarrier.ManagedCarrierIdentity, hostrelation.ExpectedRelation) {
	t.Helper()
	return testManagedCarrierIdentityForScope(
		t,
		subject,
		subjectKey,
		target.ScopeProject,
	)
}

func testManagedCarrierIdentityForScope(
	t *testing.T,
	subject topology.SubjectID,
	subjectKey hostrelation.SubjectKey,
	scope target.Scope,
) (durablecarrier.ManagedCarrierIdentity, hostrelation.ExpectedRelation) {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"context7@market",
	)
	if err != nil {
		t.Fatalf("NewSourceRef: %v", err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		scope,
		source,
	)
	if err != nil {
		t.Fatalf("NewCarrierKey: %v", err)
	}
	carrier, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	expected, err := hostrelation.Derive(key, subject, subjectKey)
	if err != nil {
		t.Fatalf("Derive expected relation: %v", err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatalf("NewManagedCarrierIdentity: %v", err)
	}
	return identity, expected
}
