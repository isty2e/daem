package mcp

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/topology"
)

func TestClassifyEffectiveCapabilityDoesNotInferUnsupportedFromMissingEvidence(t *testing.T) {
	native := effectiveCapabilityNativeContract(t)
	qualified, err := NewHostPrerequisiteObservation(HostPrerequisiteObservationInput{State: HostQualified})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name      string
		contract  lock.LockedSubjectContract
		hosts     map[topology.SubjectID]HostPrerequisiteObservation
		shadowing map[topology.SubjectID]ShadowState
		want      string
		wantError bool
	}{
		{name: "unsupported Claude observer", contract: claudeMCPRecord(t), want: "EFFECTIVE_OBSERVER_UNSUPPORTED"},
		{name: "supported Pi observer without evidence", contract: native, hosts: map[topology.SubjectID]HostPrerequisiteObservation{native.SubjectID(): qualified}, want: "EFFECTIVE_STATE_UNOBSERVED"},
		{name: "supported Pi observer unobservable", contract: native, hosts: map[topology.SubjectID]HostPrerequisiteObservation{native.SubjectID(): qualified}, shadowing: map[topology.SubjectID]ShadowState{native.SubjectID(): ShadowUnknown}, want: "EFFECTIVE_STATE_UNOBSERVED"},
		{name: "supported Pi exact", contract: native, hosts: map[topology.SubjectID]HostPrerequisiteObservation{native.SubjectID(): qualified}, shadowing: map[topology.SubjectID]ShadowState{native.SubjectID(): ShadowUnshadowed}},
		{name: "supported Pi conflict", contract: native, hosts: map[topology.SubjectID]HostPrerequisiteObservation{native.SubjectID(): qualified}, shadowing: map[topology.SubjectID]ShadowState{native.SubjectID(): ShadowShadowedByLocal}, want: "CONFIG_SHADOWED"},
		{name: "supported Pi cannot become unsupported", contract: native, hosts: map[topology.SubjectID]HostPrerequisiteObservation{native.SubjectID(): qualified}, shadowing: map[topology.SubjectID]ShadowState{native.SubjectID(): ShadowObserverUnsupported}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence, preconditions := freshMissingProjectionEvidence(t, test.contract)
			observations, err := ClassifyLockedProjections(LockedProjectionBatchInput{
				Contracts: []lock.LockedSubjectContract{test.contract}, Evidence: []observe.AggregateEvidence{evidence}, Preconditions: preconditions, Hosts: test.hosts, Shadowing: test.shadowing,
			})
			if test.wantError {
				if err == nil || len(observations) != 0 {
					t.Fatalf("contradictory capability evidence was published: %#v, %v", observations, err)
				}
				return
			}
			if err != nil || len(observations) != 1 {
				t.Fatalf("classify = %#v, %v", observations, err)
			}
			if got := string(observations[0].Current().Shadowing.Reason); got != test.want {
				t.Fatalf("effective reason = %q, want %q", got, test.want)
			}
		})
	}
}

func TestUnsupportedEffectiveObserverRejectsUnexpectedEvidence(t *testing.T) {
	contract := claudeMCPRecord(t)
	evidence, preconditions := freshMissingProjectionEvidence(t, contract)
	observations, err := ClassifyLockedProjections(LockedProjectionBatchInput{
		Contracts: []lock.LockedSubjectContract{contract}, Evidence: []observe.AggregateEvidence{evidence}, Preconditions: preconditions,
		Shadowing: map[topology.SubjectID]ShadowState{contract.SubjectID(): ShadowUnknown},
	})
	if err == nil || len(observations) != 0 {
		t.Fatalf("unsupported capability accepted foreign observation evidence: %#v, %v", observations, err)
	}
}

func effectiveCapabilityNativeContract(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	manifest, err := declarationmanifest.Decode([]byte("version = 1\ntargets = [\"pi\"]\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"project\"\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(manifest.MCPServers(), nil, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("Native fixture = %#v, %v", contracts, err)
	}
	return contracts[0]
}
