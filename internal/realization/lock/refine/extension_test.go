package refine

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/extension"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func TestExtensionLockedSubjectContractRequiresTopologyLoweringAndGraphMembership(t *testing.T) {
	value := desiredtest.Extension(t, extension.Spec{
		Name:    "context7-managed",
		Carrier: extension.CarrierClaudeCodePlugin,
		Target:  target.TargetClaudeCode,
		Scope:   target.ScopeProject,
		Source:  desiredtest.ExtensionSource(t, extension.SourceKindMarketplace, "context7@official"),
	})
	empty, err := topology.NewGraph(nil, nil)
	if err != nil {
		t.Fatalf("NewGraph returned error: %v", err)
	}
	if _, err := extensionLockedSubjectContract(empty, value); err == nil || !strings.Contains(err.Error(), "topology is missing relation subject") {
		t.Fatalf("extensionLockedSubjectContract missing graph error = %v", err)
	}

	model, err := extensiontopology.Lower([]extension.Extension{value})
	if err != nil {
		t.Fatalf("extension topology lowering returned error: %v", err)
	}
	record, err := extensionLockedSubjectContract(model.Graph(), value)
	if err != nil {
		t.Fatalf("extensionLockedSubjectContract returned error: %v", err)
	}
	want, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatalf("extension relation lowering returned error: %v", err)
	}
	if record.SubjectID() != want {
		t.Fatalf("locked subject = %s, want topology relation %s", record.SubjectID(), want)
	}
}
