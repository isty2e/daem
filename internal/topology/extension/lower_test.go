package extension_test

import (
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestRelationLowersEveryExtensionCarrierIntoItsStructuralNamespace(t *testing.T) {
	tests := []struct {
		name       string
		carrier    desiredextension.Carrier
		target     target.Target
		scope      target.Scope
		sourceKind desiredextension.SourceKind
		sourceRef  string
		namespace  string
	}{
		{name: "claude", carrier: desiredextension.CarrierClaudeCodePlugin, target: target.TargetClaudeCode, scope: target.ScopeProject, sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "context7@official", namespace: "claude-code.plugin-carrier"},
		{name: "codex", carrier: desiredextension.CarrierCodexPlugin, target: target.TargetCodex, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindMarketplace, sourceRef: "documents@openai", namespace: "codex.plugin-carrier"},
		{name: "opencode", carrier: desiredextension.CarrierOpenCodePlugin, target: target.TargetOpenCode, scope: target.ScopeProject, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "@acme/formatter", namespace: "opencode.plugin-carrier"},
		{name: "pi", carrier: desiredextension.CarrierPiPackage, target: target.TargetPi, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "github:acme/pi-tools", namespace: "pi.package-carrier"},
		{name: "antigravity", carrier: desiredextension.CarrierAntigravityCLIPlugin, target: target.TargetAntigravityCLI, scope: target.ScopeGlobal, sourceKind: desiredextension.SourceKindHostSource, sourceRef: "modern-web-guidance@google", namespace: "antigravity-cli.plugin-carrier"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := extensionValue(t, "shared-key", test.carrier, test.target, test.scope, test.sourceKind, test.sourceRef)
			subject, err := extensiontopology.Relation(value)
			if err != nil {
				t.Fatalf("Relation returned error: %v", err)
			}
			if subject.Kind() != topology.SubjectHostRelation || subject.Namespace() != test.namespace || subject.Key() != "shared-key" {
				t.Fatalf("Relation = %s, want host_relation/%s/shared-key", subject, test.namespace)
			}
			if !extensiontopology.IsCarrierRelation(test.carrier, subject) {
				t.Fatalf("IsCarrierRelation(%q, %s) = false", test.carrier, subject)
			}
			for _, other := range tests {
				if other.carrier != test.carrier && extensiontopology.IsCarrierRelation(other.carrier, subject) {
					t.Fatalf("IsCarrierRelation(%q, %s) = true for foreign carrier", other.carrier, subject)
				}
			}
		})
	}
}

func TestLowerBuildsOrderIndependentGraphWithSharedCarriers(t *testing.T) {
	first := extensionValue(t, "alpha", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official")
	second := extensionValue(t, "beta", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official")

	forward, err := extensiontopology.Lower([]desiredextension.Extension{first, second})
	if err != nil {
		t.Fatalf("Lower(forward) returned error: %v", err)
	}
	reverse, err := extensiontopology.Lower([]desiredextension.Extension{second, first})
	if err != nil {
		t.Fatalf("Lower(reverse) returned error: %v", err)
	}
	if got, want := forward.Graph().Subjects(), reverse.Graph().Subjects(); !equalSubjects(got, want) {
		t.Fatalf("subject order depends on input order: forward=%v reverse=%v", got, want)
	}
	if len(forward.Graph().Subjects()) != 3 {
		t.Fatalf("Lower subjects = %v, want two relations and one shared carrier", forward.Graph().Subjects())
	}

	carrier, err := extensiontopology.NewCarrier(first.CarrierKey())
	if err != nil {
		t.Fatalf("NewCarrier: %v", err)
	}
	firstRelation, err := extensiontopology.Relation(first)
	if err != nil {
		t.Fatalf("Relation(first): %v", err)
	}
	secondRelation, err := extensiontopology.Relation(second)
	if err != nil {
		t.Fatalf("Relation(second): %v", err)
	}
	if got, want := forward.Graph().ConsumersOf(carrier.SubjectID()), []topology.SubjectID{firstRelation, secondRelation}; !equalSubjects(got, want) {
		t.Fatalf("carrier consumers = %v, want %v", got, want)
	}
}

func TestLowerRejectsDistinctCarriersWithOneHostVisibleRelation(t *testing.T) {
	first := extensionValue(t, "alpha", desiredextension.CarrierAntigravityCLIPlugin, target.TargetAntigravityCLI, target.ScopeGlobal, desiredextension.SourceKindHostSource, "guidance@alpha")
	second := extensionValue(t, "beta", desiredextension.CarrierAntigravityCLIPlugin, target.TargetAntigravityCLI, target.ScopeGlobal, desiredextension.SourceKindHostSource, "guidance@beta")

	_, forwardErr := extensiontopology.Lower([]desiredextension.Extension{first, second})
	_, reverseErr := extensiontopology.Lower([]desiredextension.Extension{second, first})
	if forwardErr == nil || reverseErr == nil {
		t.Fatal("Lower accepted distinct carriers with one host-visible relation")
	}
	if forwardErr.Error() != reverseErr.Error() ||
		!strings.Contains(forwardErr.Error(), "same host relation") {
		t.Fatalf("relation-address collision is unstable: %q / %q", forwardErr, reverseErr)
	}
}

func TestLowerKeepsEqualRelationKeysSeparateAcrossCarrierNamespaces(t *testing.T) {
	claude := extensionValue(t, "shared-key", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official")
	codex := extensionValue(t, "shared-key", desiredextension.CarrierCodexPlugin, target.TargetCodex, target.ScopeGlobal, desiredextension.SourceKindMarketplace, "context7@official")

	model, err := extensiontopology.Lower([]desiredextension.Extension{claude, codex})
	if err != nil {
		t.Fatalf("Lower returned error: %v", err)
	}
	if len(model.Graph().Subjects()) != 4 {
		t.Fatalf("Lower subjects = %v, want two relations and two carriers", model.Graph().Subjects())
	}
	for _, value := range []desiredextension.Extension{claude, codex} {
		subject, err := extensiontopology.Relation(value)
		if err != nil {
			t.Fatalf("Relation returned error: %v", err)
		}
		if !model.Graph().Contains(subject) {
			t.Fatalf("Lower graph is missing %s", subject)
		}
	}
}

func TestLowerRejectsOneRelationSelectingDifferentCarriers(t *testing.T) {
	first := extensionValue(t, "shared", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official")
	second := extensionValue(t, "shared", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@private")

	if _, err := extensiontopology.Lower([]desiredextension.Extension{first, second}); err == nil ||
		!strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("Lower duplicate relation error = %v, want duplicate rejection", err)
	}
}

func TestLowerDoesNotGrantConsumersToForeignCarrier(t *testing.T) {
	value := extensionValue(t, "managed", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official")
	model, err := extensiontopology.Lower([]desiredextension.Extension{value})
	if err != nil {
		t.Fatalf("Lower returned error: %v", err)
	}
	foreignValue := extensionValue(t, "foreign", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@private")
	foreign, err := extensiontopology.NewCarrier(foreignValue.CarrierKey())
	if err != nil {
		t.Fatalf("NewCarrier(foreign): %v", err)
	}
	if consumers := model.Graph().ConsumersOf(foreign.SubjectID()); len(consumers) != 0 {
		t.Fatalf("foreign carrier consumers = %v, want none", consumers)
	}
}

func TestLowerRejectsDuplicateAndInvalidDesiredValues(t *testing.T) {
	claude := extensionValue(t, "context7", desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode, target.ScopeProject, desiredextension.SourceKindMarketplace, "context7@official")
	codex := extensionValue(t, "documents", desiredextension.CarrierCodexPlugin, target.TargetCodex, target.ScopeGlobal, desiredextension.SourceKindMarketplace, "documents@openai")

	_, forwardErr := extensiontopology.Lower([]desiredextension.Extension{claude, claude, codex, codex})
	_, reverseErr := extensiontopology.Lower([]desiredextension.Extension{codex, codex, claude, claude})
	if forwardErr == nil || reverseErr == nil {
		t.Fatal("Lower accepted duplicate structural subjects")
	}
	if forwardErr.Error() != reverseErr.Error() || !strings.Contains(forwardErr.Error(), "duplicate") {
		t.Fatalf("duplicate failure is unstable: %q / %q", forwardErr, reverseErr)
	}
	if _, err := extensiontopology.Lower([]desiredextension.Extension{{}}); err == nil {
		t.Fatal("Lower accepted zero desired extension")
	}
}

func TestRelationRejectsInvalidDesiredAndForeignSubjects(t *testing.T) {
	if _, err := extensiontopology.Relation(desiredextension.Extension{}); err == nil {
		t.Fatal("Relation accepted zero desired extension")
	}
	if extensiontopology.IsCarrierRelation(desiredextension.CarrierClaudeCodePlugin, topology.SubjectID{}) {
		t.Fatal("IsCarrierRelation accepted zero topology subject")
	}
	foreign, err := topology.NewSubjectID(topology.SubjectProjection, "claude-code.plugin-carrier", "context7")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	if extensiontopology.IsCarrierRelation(desiredextension.CarrierClaudeCodePlugin, foreign) {
		t.Fatal("IsCarrierRelation accepted non-relation subject kind")
	}
	valid, err := topology.NewSubjectID(topology.SubjectHostRelation, "claude-code.plugin-carrier", "context7")
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	if extensiontopology.IsCarrierRelation(desiredextension.Carrier("future-carrier"), valid) {
		t.Fatal("IsCarrierRelation accepted unsupported carrier")
	}
}

func extensionValue(
	t *testing.T,
	name string,
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	scope target.Scope,
	sourceKind desiredextension.SourceKind,
	sourceRef string,
) desiredextension.Extension {
	t.Helper()
	return desiredtest.Extension(t, desiredextension.Spec{
		Name:    name,
		Carrier: carrier,
		Target:  selectedTarget,
		Scope:   scope,
		Source:  desiredtest.ExtensionSource(t, sourceKind, sourceRef),
	})
}

func equalSubjects(left []topology.SubjectID, right []topology.SubjectID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
