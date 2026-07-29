package host

import (
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func TestObserveIgnoresRetiringDirectHostProjection(t *testing.T) {
	placement, admitted := aggregate.ImplementedMCPPlacement(
		target.TargetCodex,
		target.ScopeProject,
	)
	if !admitted {
		t.Fatal("Codex project MCP placement is not admitted")
	}
	contribution, err := placement.Contribution("context7", `{"command":"node"}`)
	if err != nil {
		t.Fatalf("construct Codex MCP contribution: %v", err)
	}
	subject, err := topologymcp.ProjectionSubject(
		target.TargetCodex,
		target.ScopeProject,
		"context7",
	)
	if err != nil {
		t.Fatalf("construct Codex MCP subject: %v", err)
	}
	projection, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatalf("construct Codex MCP projection: %v", err)
	}

	called := false
	observations, err := Observe(Input{
		Retiring: []aggregate.SubjectContribution{projection},
		ResolveDestination: func(output.Destination) (string, error) {
			called = true
			return "", fmt.Errorf("must not resolve direct-host projection")
		},
	})
	if err != nil {
		t.Fatalf("Observe returned error: %v", err)
	}
	if called {
		t.Fatal("Observe resolved a retiring direct-host projection")
	}
	if len(observations.Current) != 0 || len(observations.Retiring) != 0 {
		t.Fatalf("Observe returned direct-host evidence: %#v", observations)
	}
}
