package mcp

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/topology"
)

func TestHostPrerequisiteObservationAdmitsOnlyCoherentEvidence(t *testing.T) {
	for _, state := range []HostPrerequisiteState{HostNotApplicable, HostQualified} {
		observation, err := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: state})
		if err != nil || observation.State() != state || observation.Reason() != ReasonNone || observation.Detail() != "" {
			t.Fatalf("non-failing host evidence = %#v, %v", observation, err)
		}
	}
	for _, reason := range []ReasonCode{ReasonHostVersionUnqualified, ReasonHostSettingsUnobserved, ReasonHostBuiltinDisabled, ReasonHostBuiltinUnobserved, ReasonHostAdapterConfigured} {
		observation, err := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: HostUnqualified, Reason: reason, Detail: "repair the prerequisite"})
		if err != nil || observation.State() != HostUnqualified || observation.Reason() != reason || observation.Detail() != "repair the prerequisite" {
			t.Fatalf("causal host evidence = %#v, %v", observation, err)
		}
	}

	for _, input := range []HostPrerequisiteObservationInput{
		{},
		{State: HostQualified, Reason: ReasonHostVersionUnqualified},
		{State: HostNotApplicable, Detail: "failure"},
		{State: HostUnqualified, Detail: "missing cause"},
		{State: HostUnqualified, Reason: ReasonHostAdapterConfigured},
		{State: HostUnqualified, Reason: "unknown", Detail: "unsupported cause"},
		{State: HostUnqualified, Reason: ReasonHostAdapterConfigured, Detail: "  "},
		{State: HostUnqualified, Reason: ReasonHostAdapterConfigured, Detail: strings.Repeat("x", 2049)},
	} {
		if _, err := NewHostPrerequisiteObservation(input); err == nil {
			t.Fatalf("incoherent host evidence was admitted: %#v", input)
		}
	}
}

func TestHostPrerequisiteRequiredForNativeClassification(t *testing.T) {
	manifest, err := declarationmanifest.Decode([]byte("version = 1\ntargets = [\"pi\"]\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"project\"\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(manifest.MCPServers(), nil, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("Native fixture admission: %v", err)
	}
	contract := contracts[0]
	evidence, preconditions := freshMissingProjectionEvidence(t, contract)
	notApplicable, _ := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: HostNotApplicable})
	qualified, _ := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: HostQualified})
	unqualified, _ := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: HostUnqualified, Reason: ReasonHostAdapterConfigured, Detail: "remove the configured Adapter"})
	for _, test := range []struct {
		name     string
		hosts    map[topology.SubjectID]HostPrerequisiteObservation
		accepted bool
	}{
		{"missing", nil, false},
		{"zero value", map[topology.SubjectID]HostPrerequisiteObservation{contract.SubjectID(): {}}, false},
		{"not applicable", map[topology.SubjectID]HostPrerequisiteObservation{contract.SubjectID(): notApplicable}, false},
		{"qualified", map[topology.SubjectID]HostPrerequisiteObservation{contract.SubjectID(): qualified}, true},
		{"unqualified", map[topology.SubjectID]HostPrerequisiteObservation{contract.SubjectID(): unqualified}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			observations, err := ClassifyLockedProjections(LockedProjectionBatchInput{
				Contracts: []lock.LockedSubjectContract{contract}, Evidence: []observe.AggregateEvidence{evidence}, Preconditions: preconditions, Hosts: test.hosts,
			})
			if (err == nil) != test.accepted {
				t.Fatalf("Native host evidence acceptance = %v; wanted %t", err, test.accepted)
			}
			if test.accepted && (len(observations) != 1 || observations[0].Host() != test.hosts[contract.SubjectID()] || observations[0].Current().Projection.State != ProjectionMissing) {
				t.Fatalf("host evidence replaced independent config evidence: %#v", observations)
			}
		})
	}
}

func TestHostPrerequisiteRejectsEvidenceForNonNativeProjection(t *testing.T) {
	contract := claudeMCPRecord(t)
	evidence, preconditions := freshMissingProjectionEvidence(t, contract)
	qualified, _ := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: HostQualified})
	input := LockedProjectionBatchInput{Contracts: []lock.LockedSubjectContract{contract}, Evidence: []observe.AggregateEvidence{evidence}, Preconditions: preconditions}
	observations, err := ClassifyLockedProjections(input)
	if err != nil || len(observations) != 1 || observations[0].Host().State() != HostNotApplicable {
		t.Fatalf("non-Native host applicability = %#v, %v", observations, err)
	}
	input.Hosts = map[topology.SubjectID]HostPrerequisiteObservation{contract.SubjectID(): qualified}
	if _, err := ClassifyLockedProjections(input); err == nil {
		t.Fatal("foreign host qualification was admitted")
	}
}
