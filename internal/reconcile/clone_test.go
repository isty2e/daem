package reconcile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestResultCloneIsIndependentFromOuterSliceMutation(t *testing.T) {
	subject, err := topology.NewSubjectID(topology.SubjectProjection, "test.plan", "one")
	if err != nil {
		t.Fatal(err)
	}
	decision := newManagedPathNoOp(managedPathDecisionFacts{
		subject:         subject,
		consumerTargets: []target.Target{target.TargetCodex},
		scope:           target.ScopeProject,
		destination:     outputtest.Parse(t, "AGENTS.md"),
	}, ReasonAlreadyCurrent)
	input := []ManagedPathDecision{decision}
	original := mustReconciliationResult(t, input, nil)
	input[0] = ManagedPathDecision{}

	cloned := original.Clone()
	clonedPaths := cloned.ManagedPaths()
	clonedPaths[0] = ManagedPathDecision{}

	originalPaths := original.ManagedPaths()
	if len(originalPaths) != 1 || originalPaths[0].Kind() != ManagedPathNoOp ||
		len(original.Aggregates()) != 0 {
		t.Fatal("constructor input or returned accessor mutated the source plan")
	}
}

func TestNewResultRejectsMissingAndDuplicateManagedPathVariants(t *testing.T) {
	if _, err := NewResult(ResultInput{Context: ContextInspect, ManagedPaths: []ManagedPathDecision{{}}}); err == nil {
		t.Fatal("NewResult accepted a managed path decision without a variant")
	}

	subject, err := topology.NewSubjectID(topology.SubjectProjection, "test.plan", "duplicate")
	if err != nil {
		t.Fatal(err)
	}
	decision := newManagedPathNoOp(managedPathDecisionFacts{
		subject:         subject,
		consumerTargets: []target.Target{target.TargetCodex},
		scope:           target.ScopeProject,
		destination:     outputtest.Parse(t, "AGENTS.md"),
	}, ReasonAlreadyCurrent)
	if _, err := NewResult(ResultInput{Context: ContextInspect, ManagedPaths: []ManagedPathDecision{decision, decision}}); err == nil {
		t.Fatal("NewResult accepted duplicate managed path decisions")
	}
}

func TestNewResultRejectsMalformedManagedPathDecisions(t *testing.T) {
	projection, err := topology.NewSubjectID(topology.SubjectProjection, "test.plan", "valid")
	if err != nil {
		t.Fatal(err)
	}
	resource, err := topology.NewSubjectID(topology.SubjectResource, "test.plan", "wrong-kind")
	if err != nil {
		t.Fatal(err)
	}
	validFacts := managedPathDecisionFacts{
		subject:         projection,
		consumerTargets: []target.Target{target.TargetCodex},
		scope:           target.ScopeProject,
		destination:     outputtest.Parse(t, "AGENTS.md"),
	}
	tests := []struct {
		name     string
		decision ManagedPathDecision
		want     string
	}{
		{
			name:     "missing variant",
			decision: ManagedPathDecision{},
			want:     "requires exactly one variant",
		},
		{
			name: "multiple variants",
			decision: ManagedPathDecision{
				noOp:    &managedPathNoOpDecision{facts: validFacts},
				blocked: &managedPathBlockedDecision{facts: validFacts},
			},
			want: "requires exactly one variant",
		},
		{
			name: "wrong subject kind",
			decision: newManagedPathNoOp(managedPathDecisionFacts{
				subject: resource, consumerTargets: validFacts.consumerTargets,
				scope: validFacts.scope, destination: validFacts.destination,
			}, ReasonAlreadyCurrent),
			want: "is not a projection",
		},
		{
			name: "invalid scope",
			decision: newManagedPathNoOp(managedPathDecisionFacts{
				subject: projection, consumerTargets: validFacts.consumerTargets,
				scope: "workspace", destination: validFacts.destination,
			}, ReasonAlreadyCurrent),
			want: "unknown scope",
		},
		{
			name: "scope destination contradiction",
			decision: newManagedPathNoOp(managedPathDecisionFacts{
				subject: projection, consumerTargets: validFacts.consumerTargets,
				scope: target.ScopeGlobal, destination: validFacts.destination,
			}, ReasonAlreadyCurrent),
			want: "global destination must be home-relative or data-root-relative",
		},
		{
			name: "no current or previous owner",
			decision: newManagedPathNoOp(managedPathDecisionFacts{
				subject: projection, scope: validFacts.scope, destination: validFacts.destination,
			}, ReasonAlreadyCurrent),
			want: "current consumer target or previous managed state",
		},
		{
			name: "duplicate consumer",
			decision: newManagedPathNoOp(managedPathDecisionFacts{
				subject: projection,
				consumerTargets: []target.Target{
					target.TargetCodex,
					target.TargetCodex,
				},
				scope: validFacts.scope, destination: validFacts.destination,
			}, ReasonAlreadyCurrent),
			want: "duplicate consumer target",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewResult(ResultInput{Context: ContextInspect, ManagedPaths: []ManagedPathDecision{test.decision}})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewResult error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestNewResultAcceptsRemovalWithOnlyPreviousConsumerAuthority(t *testing.T) {
	entityID, err := entity.New(entity.KindInstructions, "removed")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(entityID, "instructions.project.agents")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, "AGENTS.md"),
		artifact.HashFileContent([]byte("previous")),
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o600,
	)
	if err != nil {
		t.Fatal(err)
	}
	decision := newManagedPathRemove(managedPathDecisionFacts{
		subject:     subject,
		scope:       target.ScopeProject,
		destination: outputtest.Parse(t, "AGENTS.md"),
		previous:    &previous,
	}, ReasonRemovedFromManifest)
	if _, err := NewResult(ResultInput{Context: ContextInspect, ManagedPaths: []ManagedPathDecision{decision}}); err != nil {
		t.Fatalf("NewResult rejected previous-state-backed removal: %v", err)
	}
}
