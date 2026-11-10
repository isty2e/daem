package nativeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

func TestCaptureHistoryRequiresSelectedActiveClaims(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, ownershipKind := range []string{"document", "keys"} {
			t.Run(string(scope)+"/"+ownershipKind, func(t *testing.T) {
				fixture := newCaptureHistoryFixture(t, scope, ownershipKind)
				for _, present := range []bool{false, true} {
					name := "missing"
					var claims []ownership.Claim
					if present {
						name, claims = "active", []ownership.Claim{fixture.claim}
					}
					t.Run(name, func(t *testing.T) {
						input := fixture.observe(t, claims)
						handoff, err := QualifyCaptureOwnership(t.Context(), input, fixture.config, fixture.placement, []output.Footprint{fixture.footprint})
						if !present {
							if !errors.Is(err, ownership.ErrClaimMissing) || handoff {
								t.Fatal("capture admitted retained native history without its selected active claim", handoff, err)
							}
							return
						}
						if err != nil || handoff {
							t.Fatal("matching native history and active ownership did not qualify", handoff, err)
						}
					})
				}
			})
		}
	}
}

type captureHistoryFixture struct {
	paths     daempaths.Paths
	config    hostconfig.Config
	placement configuration.Placement
	address   aggregate.DocumentAddress
	footprint output.Footprint
	binding   ownership.DocumentBinding
	claim     ownership.Claim
}

func newCaptureHistoryFixture(t *testing.T, scope target.Scope, ownershipKind string) captureHistoryFixture {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("HOME", home)
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	paths, err = paths.WithDataDir(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	source := "source.json"
	if scope == target.ScopeGlobal {
		source = filepath.Join(root, source)
	}
	environment, err := declarationmanifest.Decode([]byte(fmt.Sprintf(`version = 1
targets = ["codex"]
[[host_config]]
id = "selected"
scope = %q
path = "selected.json"
source = %q
ownership = %q
`, scope, source, ownershipKind)))
	if err != nil {
		t.Fatal(err)
	}
	config := environment.HostConfigs()[0]
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{
		Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
	})
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(config.Target(), scope, placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	hostRoot := root
	if scope == target.ScopeGlobal {
		hostRoot = home
	}
	host := filepath.Join(hostRoot, ".codex", "selected.json")
	content := []byte(`{"theme":"wanted"}`)
	for _, path := range []string{filepath.Dir(host), filepath.Dir(paths.StatefilePath), filepath.Dir(paths.OwnershipRegistryPath)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(host, content, 0o600); err != nil {
		t.Fatal(err)
	}

	var input durable.SnapshotInput
	footprint := output.WholePathFootprint()
	if ownershipKind == "document" {
		subject, err := aggregate.HostConfigDocumentSubject(config.ID(), config.Target(), scope)
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedPathState(subject, []target.Target{config.Target()}, scope, placement.Destination(), artifact.HashFileContent(content), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		input.ManagedPaths = []durable.ManagedPathState{row}
	} else {
		key, err := output.NewKeyPath("theme")
		if err != nil {
			t.Fatal(err)
		}
		footprint, err = output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(config.Target(), scope, placement.Destination(), footprint, "native-json-v1")
		if err != nil {
			t.Fatal(err)
		}
		document, err := configdocument.Parse(t.Context(), content, configdocument.JSON, DocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		value, present, err := document.Lookup(key)
		if err != nil || !present {
			t.Fatal("fixture has no native history value", err)
		}
		canonical, err := value.CanonicalContext(t.Context(), DocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		contribution, err := aggregate.NewManagedContribution(contract, canonical)
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologyprojection.HostConfigKeySubject(config.ID(), config.Target(), scope, key)
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedAggregateState(subject, contribution)
		if err != nil {
			t.Fatal(err)
		}
		input.ManagedAggregates = []durable.ManagedAggregateState{row}
	}
	before, err := durable.NewSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	codecs, err := Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.StatefilePath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	defer observed.Close()
	facts, present := observed.NativeInput()
	if !present {
		t.Fatal("selected capture did not retain its observation")
	}
	exact, present := facts.Documents.Evidence().Namespaces()[0].Observation().Exact()
	if !present {
		t.Fatal("capture fixture did not observe an existing document")
	}
	binding, err := ownership.NewDocumentBinding(facts.Owner, placement.Destination(), exact)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := ownership.NewManagedAddress(exact, footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(managed, facts.Owner)
	if err != nil {
		t.Fatal(err)
	}
	return captureHistoryFixture{paths: paths, config: config, placement: placement, address: address, footprint: footprint, binding: binding, claim: claim}
}

func (fixture captureHistoryFixture) observe(t *testing.T, claims []ownership.Claim) readiness.NativeAssessmentInput {
	t.Helper()
	registry, err := ownership.NewRegistry(claims, fixture.binding)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.OwnershipRegistryPath, content, 0o600); err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), fixture.paths)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := ObserveSelectedDocuments(t.Context(), fixture.paths, &epoch, []aggregate.DocumentAddress{fixture.address})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := observed.Close(); err != nil {
			t.Error(err)
		}
	})
	facts, present := observed.NativeInput()
	if !present {
		t.Fatal("capture history has no retained native observation")
	}
	return facts
}
