//go:build darwin

package ownership

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildPreservesForeignAncestorClaimForProvisionalDestination(t *testing.T) {
	root := canonicalRoot(t)
	ancestorPath := filepath.Join(root, "host")
	if err := os.MkdirAll(ancestorPath, 0o700); err != nil {
		t.Fatal(err)
	}
	candidatePath := filepath.Join(ancestorPath, "Caf\u00e9")
	destination := outputtest.Parse(t, "~/.agents/skills/example")

	owner, err := stateauthority.New(
		pathtest.DarwinCaseSensitive(filepath.Join(root, "foreign", "state.json")),
		filepath.Join(root, "foreign", "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	ancestorAuthority, err := mutation.ObservePersistedDirectoryEntryAuthority(ancestorPath)
	if err != nil {
		t.Fatal(err)
	}
	address, err := outputownership.NewManagedAddress(ancestorAuthority.Exact(), "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := outputownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := outputownership.NewRegistry([]outputownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	pathObservation, err := mutation.ObserveDirectoryEntryAuthority(candidatePath)
	if err != nil {
		t.Fatal(err)
	}
	provisionalPath, provisional := pathObservation.Provisional()
	if !provisional {
		t.Fatal("missing normalization-sensitive destination was not provisional")
	}
	if !provisionalPath.CandidateWithin(address.PathAuthority()) {
		t.Fatalf(
			"candidate %q was not within claim path %q",
			provisionalPath.CandidateKey(),
			address.Path(),
		)
	}
	selection, err := targetselection.ForDiagnostics([]string{string(target.TargetCodex)})
	if err != nil {
		t.Fatal(err)
	}

	result, err := Build(Input{
		Paths: testPaths(root),
		Resolver: func(output.Destination) (string, error) {
			return candidatePath, nil
		},
		ManagedPaths: []ManagedPathInput{{
			Scope: target.ScopeGlobal, Destination: destination, ConsumerTargets: []target.Target{target.TargetCodex},
		}},
		Selection: selection,
		Registry:  registry,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(result.Observations))
	}
	observation := result.Observations[0]
	if _, provisional := observation.ProvisionalPath(); !provisional {
		t.Fatal("missing normalization-sensitive destination was not provisional")
	}
	observed, present := observation.Claim().Get()
	if !present || !observed.Equal(claim) {
		t.Fatalf("observed claim = %#v, present=%t; want ancestor claim", observed, present)
	}
}
