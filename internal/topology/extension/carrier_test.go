package extension_test

import (
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestCarrierIdentitySeparatesScopeSourceAndFamily(t *testing.T) {
	carriers := []extensiontopology.Carrier{
		carrier(t, desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official"),
		carrier(t, desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeGlobal, desiredextension.SourceKindMarketplace, "context7@official"),
		carrier(t, desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@private"),
		carrier(t, desiredextension.CarrierCodexPlugin, target.TargetCodex, target.ScopeGlobal, desiredextension.SourceKindMarketplace, "context7@official"),
	}

	seen := make(map[topology.SubjectID]struct{}, len(carriers))
	for _, value := range carriers {
		if _, duplicate := seen[value.SubjectID()]; duplicate {
			t.Fatalf("carrier identity collapsed for %s", value.SubjectID())
		}
		seen[value.SubjectID()] = struct{}{}
	}
}

func TestCarrierIdentityRoundTripsEscapedSource(t *testing.T) {
	value := carrier(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		desiredextension.SourceKindMarketplace,
		`plugin"한글@market/name%2Fstable`,
	)
	parsed, err := topology.ParseSubjectID(value.SubjectID().String())
	if err != nil {
		t.Fatalf("ParseSubjectID(%q): %v", value.SubjectID(), err)
	}
	if parsed != value.SubjectID() {
		t.Fatalf("carrier round trip = %s, want %s", parsed, value.SubjectID())
	}
}

func TestNewCarrierRejectsZeroKey(t *testing.T) {
	if _, err := extensiontopology.NewCarrier(desiredextension.CarrierKey{}); err == nil {
		t.Fatal("NewCarrier accepted zero key")
	}
	if err := (extensiontopology.Carrier{}).Validate(); err == nil {
		t.Fatal("Carrier.Validate accepted zero carrier")
	}
}

func carrier(
	t *testing.T,
	family desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
) extensiontopology.Carrier {
	t.Helper()
	key, err := desiredextension.NewCarrierKey(
		family,
		selectedTarget,
		scope,
		desiredtest.ExtensionSource(t, sourceKind, sourceRef),
	)
	if err != nil {
		t.Fatalf("NewCarrierKey: %v", err)
	}
	value, err := extensiontopology.NewCarrier(key)
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	return value
}
