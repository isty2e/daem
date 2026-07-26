package aggregate

import (
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestManagedContributionOwnsAndCanonicalizesItsContract(t *testing.T) {
	input := testSharedHookContributionInput(`{"command":"review"}`)
	input.ComparedFields = []string{"event", "command", "event"}
	contribution := mustManagedContribution(t, input)

	if contribution.Cardinality() != ContributionSharedSet ||
		contribution.Address().Document().Target() != target.TargetCodex ||
		contribution.Address().Document().Scope() != target.ScopeProject ||
		contribution.Address().Document().AggregateRoot() != "settings.json" ||
		contribution.Address().ContentPath() != ContentPath("/hooks") {
		t.Fatalf("contribution address/contract = %#v", contribution)
	}
	if got := contribution.ComparedFields(); !reflect.DeepEqual(got, []string{"command", "event"}) {
		t.Fatalf("ComparedFields = %#v", got)
	}
	fields := contribution.ComparedFields()
	fields[0] = "mutated"
	if contribution.ComparedFields()[0] != "command" {
		t.Fatal("caller mutation changed compared fields")
	}
	if !contribution.Equal(contribution.Clone()) || !contribution.Contract().Equal(contribution.Contract()) {
		t.Fatal("valid contribution or contract did not equal its clone")
	}
}

func TestManagedContributionRejectsMalformedIndependentAxes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ManagedContributionInput)
		want   string
	}{
		{name: "placement", mutate: func(input *ManagedContributionInput) { input.PlacementID = " " }, want: "placement id"},
		{name: "placement whitespace", mutate: func(input *ManagedContributionInput) { input.PlacementID = " codex.project.hooks " }, want: "placement id"},
		{name: "target", mutate: func(input *ManagedContributionInput) { input.Target = "future" }, want: "target"},
		{name: "scope", mutate: func(input *ManagedContributionInput) { input.Scope = "workspace" }, want: "scope"},
		{name: "absolute root", mutate: func(input *ManagedContributionInput) { input.AggregateRoot = "/tmp/settings.json" }, want: "aggregate root"},
		{name: "home root in project", mutate: func(input *ManagedContributionInput) { input.AggregateRoot = "~/.config/settings.json" }, want: "selected project root"},
		{name: "relative content path", mutate: func(input *ManagedContributionInput) { input.ContentPath = "hooks" }, want: "absolute"},
		{name: "traversal content path", mutate: func(input *ManagedContributionInput) { input.ContentPath = "/hooks/../mcp" }, want: "canonical"},
		{name: "nul content path", mutate: func(input *ManagedContributionInput) { input.ContentPath = "/hooks/\x00entry" }, want: "control"},
		{name: "bidi content path", mutate: func(input *ManagedContributionInput) { input.ContentPath = "/hooks/\u202eentry" }, want: "control"},
		{name: "merge unit", mutate: func(input *ManagedContributionInput) { input.MergeUnit = "" }, want: "merge unit"},
		{name: "merge whitespace", mutate: func(input *ManagedContributionInput) { input.MergeUnit = " hook-set " }, want: "merge unit"},
		{name: "cardinality", mutate: func(input *ManagedContributionInput) { input.Cardinality = "many_if_needed" }, want: "cardinality"},
		{name: "retention", mutate: func(input *ManagedContributionInput) { input.SiblingRetention = "replace_document" }, want: "sibling retention"},
		{name: "preservation", mutate: func(input *ManagedContributionInput) { input.SiblingPreservation = "best_effort" }, want: "sibling preservation"},
		{name: "equivalence", mutate: func(input *ManagedContributionInput) { input.Equivalence = "fuzzy" }, want: "equivalence"},
		{name: "byte fields", mutate: func(input *ManagedContributionInput) { input.Equivalence = EquivalenceByteExact }, want: "must not include compared fields"},
		{name: "semantic fields", mutate: func(input *ManagedContributionInput) { input.ComparedFields = nil }, want: "values are required"},
		{name: "canonical empty", mutate: func(input *ManagedContributionInput) { input.CanonicalContribution = "" }, want: "canonical contribution is required"},
		{name: "canonical invalid utf8", mutate: func(input *ManagedContributionInput) { input.CanonicalContribution = string([]byte{0xff}) }, want: "valid UTF-8"},
		{name: "codec", mutate: func(input *ManagedContributionInput) { input.CodecContractID = "codec v1" }, want: "codec contract id"},
		{name: "codec whitespace", mutate: func(input *ManagedContributionInput) { input.CodecContractID = " codex-project-hooks-v1 " }, want: "codec contract id"},
		{name: "compared field", mutate: func(input *ManagedContributionInput) { input.ComparedFields = []string{"command value"} }, want: "compared field"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := testSharedHookContributionInput(`{"command":"review"}`)
			test.mutate(&input)
			_, err := NewManagedContribution(input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewManagedContribution error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestContributionSetSeparatesSharedAndExclusiveProjectionCardinality(t *testing.T) {
	alpha := mustSubjectContribution(t, "alpha", testSharedHookContributionInput(`{"command":"alpha"}`))
	zeta := mustSubjectContribution(t, "zeta", testSharedHookContributionInput(`{"command":"zeta"}`))

	set, err := NewContributionSet([]SubjectContribution{zeta, alpha})
	if err != nil {
		t.Fatalf("NewContributionSet returned error: %v", err)
	}
	got := set.Contributions()
	if len(got) != 2 || got[0].SubjectID().Key() != "alpha" || got[1].SubjectID().Key() != "zeta" {
		t.Fatalf("canonical contributions = %#v", got)
	}
	if set.Contract().Cardinality() != ContributionSharedSet || set.Address() != alpha.Contribution().Address() {
		t.Fatalf("set contract = %#v", set.Contract())
	}

	if _, err := NewContributionSet([]SubjectContribution{alpha, alpha}); err == nil || !strings.Contains(err.Error(), "repeats subject") {
		t.Fatalf("duplicate subject error = %v", err)
	}

	exclusiveInput := testSharedHookContributionInput(`{"command":"alpha"}`)
	exclusiveInput.Cardinality = ContributionExclusive
	exclusiveAlpha := mustSubjectContribution(t, "alpha", exclusiveInput)
	exclusiveZeta := mustSubjectContribution(t, "zeta", exclusiveInput)
	if _, err := NewContributionSet([]SubjectContribution{exclusiveAlpha}); err != nil {
		t.Fatalf("single exclusive contribution error = %v", err)
	}
	if _, err := NewContributionSet([]SubjectContribution{exclusiveAlpha, exclusiveZeta}); err == nil || !strings.Contains(err.Error(), "exactly one subject") {
		t.Fatalf("exclusive set error = %v", err)
	}
}

func TestOneSubjectMCPEntryUsesTheGenericExclusiveContributionSet(t *testing.T) {
	input := ManagedContributionInput{
		PlacementID:           "claude-code.project.project-config",
		Target:                target.TargetClaudeCode,
		Scope:                 target.ScopeProject,
		AggregateRoot:         ".mcp.json",
		ContentPath:           "/mcpServers/context7",
		MergeUnit:             "mcp-server-entry",
		Cardinality:           ContributionExclusive,
		SiblingRetention:      PreserveUnmanagedSiblings,
		SiblingPreservation:   PreserveSiblingsSemantic,
		Equivalence:           EquivalenceCanonicalSemantic,
		CanonicalContribution: `{"type":"stdio","command":"npx"}`,
		CodecContractID:       "claude-project-mcp-stdio-v1",
		ComparedFields:        []string{"command", "type"},
	}
	subject, err := topology.NewSubjectID(
		topology.SubjectProjection,
		"claude-code.project.mcp-server",
		"context7",
	)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewSubjectContribution(subject, mustManagedContribution(t, input))
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewContributionSet([]SubjectContribution{item})
	if err != nil {
		t.Fatalf("NewContributionSet returned error: %v", err)
	}
	if set.Contract().Cardinality() != ContributionExclusive || len(set.Contributions()) != 1 {
		t.Fatalf("MCP contribution set = %#v", set)
	}
}

func TestContributionSetRejectsStaticContractDrift(t *testing.T) {
	base := testSharedHookContributionInput(`{"command":"alpha"}`)
	alpha := mustSubjectContribution(t, "alpha", base)

	tests := []struct {
		name   string
		mutate func(*ManagedContributionInput)
		want   string
	}{
		{name: "address", mutate: func(input *ManagedContributionInput) { input.ContentPath = "/hooks/other" }, want: "mixes projection addresses"},
		{name: "placement", mutate: func(input *ManagedContributionInput) { input.PlacementID = "codex.project.other-hooks" }, want: "mixes projection addresses"},
		{name: "cardinality", mutate: func(input *ManagedContributionInput) { input.Cardinality = ContributionExclusive }, want: "codec or preservation contracts"},
		{name: "codec", mutate: func(input *ManagedContributionInput) { input.CodecContractID = "codex-project-hooks-v2" }, want: "codec or preservation contracts"},
		{name: "preservation", mutate: func(input *ManagedContributionInput) { input.SiblingPreservation = PreserveSiblingsByteExact }, want: "codec or preservation contracts"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			other := base
			other.CanonicalContribution = `{"command":"zeta"}`
			test.mutate(&other)
			zeta := mustSubjectContribution(t, "zeta", other)
			_, err := NewContributionSet([]SubjectContribution{alpha, zeta})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewContributionSet error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func testSharedHookContributionInput(canonical string) ManagedContributionInput {
	return ManagedContributionInput{
		PlacementID:           "codex.project.hooks",
		Target:                target.TargetCodex,
		Scope:                 target.ScopeProject,
		AggregateRoot:         "settings.json",
		ContentPath:           "/hooks",
		MergeUnit:             "hook-set",
		Cardinality:           ContributionSharedSet,
		SiblingRetention:      PreserveUnmanagedSiblings,
		SiblingPreservation:   PreserveSiblingsSemantic,
		Equivalence:           EquivalenceCanonicalSemantic,
		CanonicalContribution: canonical,
		CodecContractID:       "codex-project-hooks-v1",
		ComparedFields:        []string{"command", "event"},
	}
}

func mustManagedContribution(t *testing.T, input ManagedContributionInput) ManagedContribution {
	t.Helper()
	contribution, err := NewManagedContribution(input)
	if err != nil {
		t.Fatalf("NewManagedContribution returned error: %v", err)
	}
	return contribution
}

func mustSubjectContribution(t *testing.T, key string, input ManagedContributionInput) SubjectContribution {
	t.Helper()
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "codex.project.hook", key)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	item, err := NewSubjectContribution(subject, mustManagedContribution(t, input))
	if err != nil {
		t.Fatalf("NewSubjectContribution returned error: %v", err)
	}
	return item
}
