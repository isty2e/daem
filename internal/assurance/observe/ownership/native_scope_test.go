package ownership

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildKeepsNativeScopesDistinctForVisibleAndFutureDocuments(t *testing.T) {
	root := canonicalRoot(t)
	physical := filepath.Join(root, "settings.json")
	if err := os.WriteFile(physical, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := outputtest.Parse(t, "@pi-agent/settings.json")
	scopes := []output.Footprint{
		outputtest.KeyFootprint(t, ""), outputtest.KeyFootprint(t, "a/b"),
		outputtest.KeyFootprint(t, "a", "b"), outputtest.KeyFootprint(t, "a~1b"),
		outputtest.KeyFootprint(t, ".."), outputtest.KeyFootprint(t, "line\n\x00key"),
	}
	codec, admitted := aggregate.HostConfigCodecForSyntax("json")
	if !admitted {
		t.Fatal("JSON native projection grammar was not admitted")
	}
	addresses := make([]aggregate.ProjectionAddress, 0, len(scopes)+1)
	for _, scope := range scopes {
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, destination, scope, codec)
		if err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, contract.Address())
	}
	addresses = append(addresses, addresses[0])
	selection, err := targetselection.ForDiagnostics([]string{"pi"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{physical, filepath.Join(root, "future-Caf\u00e9", "settings.json")} {
		result, err := Build(Input{
			Paths: testPaths(root), Selection: selection, Aggregates: addresses,
			Resolver:     func(output.Destination) (string, error) { return path, nil },
			ManagedPaths: []ManagedPathInput{{Scope: target.ScopeGlobal, Destination: destination, ConsumerTargets: []target.Target{target.TargetPi}}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Observations) != len(scopes)+1 {
			t.Fatalf("collector lost scopes or retained duplicates: %d", len(result.Observations))
		}
		seen := make(map[output.Footprint]bool)
		for index, observation := range result.Observations {
			seen[observation.Footprint()] = true
			if index > 0 && output.CompareFootprints(result.Observations[index-1].Footprint(), observation.Footprint()) >= 0 {
				t.Fatal("ownership observations are not in canonical identity order")
			}
		}
		if !seen[output.WholePathFootprint()] {
			t.Fatal("managed file lost whole-path authority")
		}
		for _, scope := range scopes {
			if !seen[scope] {
				t.Fatal("collector reinterpreted a native key")
			}
		}
	}
}
