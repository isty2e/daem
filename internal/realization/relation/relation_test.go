package hostrelation_test

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestDeriveSeparatesVisibleAndManagedRelationIdentity(t *testing.T) {
	carrier := mustCarrierKey(t, "context7@official")
	subject := mustRelationSubject(t, "context7")
	visible := mustSubjectKey(t, "context7@official")

	relation, err := hostrelation.Derive(carrier, subject, visible)
	if err != nil {
		t.Fatalf("Derive returned error: %v", err)
	}
	if relation.SubjectKey() != visible {
		t.Fatalf("SubjectKey = %q, want %q", relation.SubjectKey(), visible)
	}
	if !strings.HasPrefix(string(relation.ManagedInstanceKey()), "host-relation:v1:") {
		t.Fatalf("ManagedInstanceKey = %q, want versioned key", relation.ManagedInstanceKey())
	}

	renamedVisible, err := hostrelation.Derive(
		carrier,
		subject,
		mustSubjectKey(t, "context7-renamed@official"),
	)
	if err != nil {
		t.Fatalf("Derive renamed visible key: %v", err)
	}
	if renamedVisible.ManagedInstanceKey() != relation.ManagedInstanceKey() {
		t.Fatal("host-visible key unexpectedly changed managed identity")
	}

	renamedDesired, err := hostrelation.Derive(
		carrier,
		mustRelationSubject(t, "context7-renamed"),
		visible,
	)
	if err != nil {
		t.Fatalf("Derive renamed desired relation: %v", err)
	}
	if renamedDesired.ManagedInstanceKey() == relation.ManagedInstanceKey() {
		t.Fatal("distinct topology relation reused managed identity")
	}
}

func TestExpectedRelationRejectsMalformedAndForeignInputs(t *testing.T) {
	carrier := mustCarrierKey(t, "context7@official")
	visible := mustSubjectKey(t, "context7@official")
	foreign, err := topology.NewSubjectID(topology.SubjectResource, "mcp", "context7")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostrelation.Derive(carrier, foreign, visible); err == nil {
		t.Fatal("Derive accepted a non-host-relation topology subject")
	}

	for _, value := range []string{"", " leading", "trailing ", "bad\nkey", "bad\u202ekey", string([]byte{0xff})} {
		if _, err := hostrelation.NewSubjectKey(value); err == nil {
			t.Fatalf("NewSubjectKey(%q) accepted malformed input", value)
		}
		if _, err := hostrelation.NewManagedInstanceKey(value); err == nil {
			t.Fatalf("NewManagedInstanceKey(%q) accepted malformed input", value)
		}
	}
}

func TestDeriveManagedIdentityCoversEveryCanonicalAxis(t *testing.T) {
	baseCarrier := mustCarrierKeyFor(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		desiredextension.SourceKindMarketplace,
		`team/plugin:beta\"한글@official`,
	)
	baseSubject := mustRelationSubject(t, "context7")
	visible := mustSubjectKey(t, "context7@official")
	baseline, err := hostrelation.Derive(baseCarrier, baseSubject, visible)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := hostrelation.Derive(baseCarrier, baseSubject, visible)
	if err != nil {
		t.Fatal(err)
	}
	if !baseline.Equal(repeated) {
		t.Fatal("identical canonical facts produced different expected relations")
	}

	otherNamespace, err := topology.NewSubjectID(
		topology.SubjectHostRelation,
		"other.plugin-carrier",
		baseSubject.Key(),
	)
	if err != nil {
		t.Fatal(err)
	}
	variants := []struct {
		name    string
		carrier desiredextension.CarrierKey
		subject topology.SubjectID
	}{
		{
			name: "scope",
			carrier: mustCarrierKeyFor(
				t,
				desiredextension.CarrierClaudeCodePlugin,
				target.TargetClaudeCode,
				target.ScopeGlobal,
				desiredextension.SourceKindMarketplace,
				`team/plugin:beta\"한글@official`,
			),
			subject: baseSubject,
		},
		{
			name: "source",
			carrier: mustCarrierKeyFor(
				t,
				desiredextension.CarrierClaudeCodePlugin,
				target.TargetClaudeCode,
				target.ScopeProject,
				desiredextension.SourceKindMarketplace,
				`team/plugin:rc\"한글@official`,
			),
			subject: baseSubject,
		},
		{
			name: "carrier and target",
			carrier: mustCarrierKeyFor(
				t,
				desiredextension.CarrierCodexPlugin,
				target.TargetCodex,
				target.ScopeGlobal,
				desiredextension.SourceKindMarketplace,
				"team-plugin-beta@official",
			),
			subject: baseSubject,
		},
		{
			name:    "subject namespace",
			carrier: baseCarrier,
			subject: otherNamespace,
		},
		{
			name:    "subject key",
			carrier: baseCarrier,
			subject: mustRelationSubject(t, "context7-renamed"),
		},
	}
	for _, test := range variants {
		t.Run(test.name, func(t *testing.T) {
			derived, err := hostrelation.Derive(test.carrier, test.subject, visible)
			if err != nil {
				t.Fatalf("Derive returned error: %v", err)
			}
			if derived.ManagedInstanceKey() == baseline.ManagedInstanceKey() {
				t.Fatalf("%s axis did not change managed identity", test.name)
			}
		})
	}

	renamedVisible, err := hostrelation.Derive(
		baseCarrier,
		baseSubject,
		mustSubjectKey(t, "renamed@official"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if renamedVisible.ManagedInstanceKey() != baseline.ManagedInstanceKey() {
		t.Fatal("host-visible key leaked into managed identity")
	}
}

func TestExpectedRelationEqualityRejectsInvalidValues(t *testing.T) {
	valid, err := hostrelation.Derive(
		mustCarrierKey(t, "context7@official"),
		mustRelationSubject(t, "context7"),
		mustSubjectKey(t, "context7@official"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if (hostrelation.ExpectedRelation{}).Equal(valid) {
		t.Fatal("zero relation compared equal to a valid relation")
	}
	if valid.Equal(hostrelation.ExpectedRelation{}) {
		t.Fatal("valid relation compared equal to a zero relation")
	}
}

func mustCarrierKey(t *testing.T, sourceRef string) desiredextension.CarrierKey {
	t.Helper()
	return mustCarrierKeyFor(
		t,
		desiredextension.CarrierClaudeCodePlugin,
		target.TargetClaudeCode,
		target.ScopeProject,
		desiredextension.SourceKindMarketplace,
		sourceRef,
	)
}

func mustCarrierKeyFor(
	t *testing.T,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
) desiredextension.CarrierKey {
	t.Helper()
	source, err := desiredextension.NewSourceRef(sourceKind, sourceRef)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		carrier,
		selectedTarget,
		scope,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func mustRelationSubject(t *testing.T, name string) topology.SubjectID {
	t.Helper()
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    name,
		Carrier: desiredextension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  mustCarrierKey(t, "context7@official").Source(),
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}

func mustSubjectKey(t *testing.T, value string) hostrelation.SubjectKey {
	t.Helper()
	key, err := hostrelation.NewSubjectKey(value)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
