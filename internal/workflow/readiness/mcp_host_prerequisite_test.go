package readiness

import (
	"path/filepath"
	"testing"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	mcpeffectivehost "github.com/isty2e/daem/internal/assurance/observe/mcp/effective/host"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/topology"
)

func TestMCPHostPrerequisiteConstraintPreservesIndependentEffectiveEvidence(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "pi.project.mcp-server", "context7")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "mcp.json")
	source, err := mcpeffective.NewSourceObservation(mcpeffective.SourceObservationInput{
		ID: "selected", Path: path, Kind: mcpeffective.SourceNormal, Precedence: mcpeffective.PrecedenceSelected,
		State: mcpeffective.SourceOpaque, Detail: "config cannot be observed",
		DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
	})
	if err != nil {
		t.Fatal(err)
	}
	effective, err := mcpeffective.NewObservation(mcpeffective.ObservationInput{Subject: subject, ServerName: "context7", SelectedPath: path, Sources: []mcpeffective.SourceObservation{source}})
	if err != nil {
		t.Fatal(err)
	}
	host, err := mcpobserve.NewHostPrerequisiteObservation(mcpobserve.HostPrerequisiteObservationInput{
		State: mcpobserve.HostUnqualified, Reason: mcpobserve.ReasonHostVersionUnqualified, Detail: "a supported Pi version is required",
	})
	if err != nil {
		t.Fatal(err)
	}
	observations := mcpeffectivehost.ObservationSet{Current: []mcpeffective.Observation{effective}, HostPrerequisites: map[topology.SubjectID]mcpobserve.HostPrerequisiteObservation{subject: host}}
	constraints, err := mcpPublicationConstraints(observations)
	if err != nil || len(constraints) != 1 || constraints[0].Subject() != subject || constraints[0].Reason() != reconcile.ReasonHostPrerequisiteUnqualified {
		t.Fatalf("competing failures prevented one binding refusal: %#v, %v", constraints, err)
	}
	if observations.Current[0].State() != mcpeffective.StateUnobservable || observations.HostPrerequisites[subject] != host {
		t.Fatal("publication precedence erased independent status evidence")
	}

	qualified, err := mcpobserve.NewHostPrerequisiteObservation(mcpobserve.HostPrerequisiteObservationInput{State: mcpobserve.HostQualified})
	if err != nil {
		t.Fatal(err)
	}
	observations.HostPrerequisites[subject] = qualified
	constraints, err = mcpPublicationConstraints(observations)
	if err != nil || len(constraints) != 1 || constraints[0].Reason() != reconcile.ReasonEffectiveStateUnobserved {
		t.Fatalf("qualified host bypassed effective-config refusal: %#v, %v", constraints, err)
	}
}
