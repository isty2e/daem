package readiness

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestMCPBackendTransitionIsDiagnosedBeforeDocumentIO(t *testing.T) {
	for _, backends := range [][2]string{{"adapter", "native"}, {"native", "adapter"}} {
		t.Run(backends[0]+"-to-"+backends[1], func(t *testing.T) {
			stored := piBackendTransitionContribution(t, backends[0])
			desired := piBackendTransitionContribution(t, backends[1])
			state, err := durable.NewManagedAggregateState(stored.SubjectID(), stored.Contribution())
			if err != nil {
				t.Fatal(err)
			}
			resolved := false
			selection, err := targetselection.ForAvailableTargets([]target.Target{target.TargetPi}, nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = observeAggregateDocuments(t.Context(), func(_ output.Destination) (string, error) {
				resolved = true
				return "", errors.New("unexpected document read")
			}, []aggregate.SubjectContribution{desired}, []durable.ManagedAggregateState{state}, selection, aggregatecodec.Catalog())
			var transition *MCPBackendTransitionError
			if !errors.As(err, &transition) || resolved {
				t.Fatalf("codec mismatch was not a pre-I/O typed refusal: %v, resolved=%t", err, resolved)
			}
			if string(transition.stored) != backends[0] || string(transition.desired) != backends[1] || transition.address != desired.Contribution().Address() {
				t.Fatalf("transition lost stored/desired roles: %#v", transition)
			}
			for _, field := range []string{"context7", "pi", "project", backends[0], backends[1]} {
				if !strings.Contains(transition.Error(), field) {
					t.Fatalf("transition omitted canonical context %q: %v", field, transition)
				}
			}
			if err := piMCPBackendTransition(stored.Contribution().Contract(), stored.Contribution().Contract()); err != nil {
				t.Fatalf("unchanged backend was labelled migration: %v", err)
			}
		})
	}
}

func piBackendTransitionContribution(t *testing.T, backend string) aggregate.SubjectContribution {
	t.Helper()
	content := "version = 1\ntargets = [\"pi\"]\n"
	if backend == "adapter" {
		content += "[[extension]]\nid = \"provider\"\ncarrier = \"pi-package\"\ntargets = [\"pi\"]\nscope = \"project\"\nsource = { host_source = \"npm:pi-mcp-adapter@^2.13.0\" }\n"
	}
	content += "[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"project\"\nbackend = \"" + backend + "\"\ntransport = \"stdio\"\ncommand = \"node\"\n"
	manifest, err := declarationmanifest.Decode([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(manifest.MCPServers(), manifest.Extensions(), mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("transition fixture admission: %v", err)
	}
	contribution, present, err := contracts[0].ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("transition contribution: %v", err)
	}
	return contribution
}
