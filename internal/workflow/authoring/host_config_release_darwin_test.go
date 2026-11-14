//go:build darwin

package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestHostConfigReleaseDoesNotUseFoldedHistoryToSelectAnOwner(t *testing.T) {
	fixture := newHostReleaseFixture(t, false)
	root := filepath.Join(t.TempDir(), "İstanbul")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(root, "daem.toml")
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(root, "state.json"), physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	exact, present := observed.Exact()
	if !present {
		t.Fatal("existing native namespace did not yield exact authority")
	}
	key, historical := exact.LegacyComparisonKey()
	if !historical {
		t.Skip("filesystem does not provide a lossy Darwin comparison profile")
	}
	witness := strings.ReplaceAll(strings.Replace(exact.Witness(), "darwin-case-v2:", "darwin-case-v1:", 1), "u", "i")
	legacy, err := pathauthority.NewExact(key, witness)
	if err != nil || !exact.MatchesLegacyComparison(legacy) || exact.Equal(legacy) {
		t.Fatal("fixture did not retain ambiguous folded history", err)
	}

	for _, sameManifest := range []bool{false, true} {
		provenance := manifest
		if !sameManifest {
			provenance = filepath.Join(root, "foreign.toml")
		}
		owner, err := stateauthority.New(legacy, provenance)
		if err != nil {
			t.Fatal(err)
		}
		claim, err := ownership.NewReservedClaim(fixture.registry.Claims()[0].Address(), owner, "legacy-foreign")
		if err != nil {
			t.Fatal(err)
		}
		registry, err := ownership.NewRegistry([]ownership.Claim{claim})
		if err != nil {
			t.Fatal(err)
		}
		selected, err := selectHostConfigReleaseOwner(manifest, observed, registry)
		if sameManifest {
			if err == nil {
				t.Fatal("same-manifest conflicting legacy State was treated as foreign")
			}
		} else if err != nil || !selected.StatefileAuthority().Equal(exact) || selected.Equal(owner) {
			t.Fatal("folded foreign history acquired current own authority", err)
		}
	}
}

func TestHostConfigReleaseCannotSelectALegacyPeerForAMissingNativeDocument(t *testing.T) {
	fixture := newHostReleaseFixture(t, false)
	writeUnmanageFile(t, fixture.paths.ManifestPath, []byte("version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='project'\npath='İstanbul/config.toml'\nsource='./missing-source.toml'\nownership='keys'\n"))
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	var rows []durable.ManagedAggregateState
	for _, selected := range []struct{ id, path string }{{"preferences", "İstanbul/config.toml"}, {"peer", "istanbul/config.toml"}} {
		placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: target.TargetCodex, Scope: target.ScopeProject, Path: selected.path, Ownership: hostconfig.OwnershipKeys})
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, placement.Destination(), footprint, "native-toml-v1")
		if err != nil {
			t.Fatal(err)
		}
		value, err := aggregate.NewManagedContribution(contract, fixture.before.ManagedAggregates()[0].Contribution().CanonicalContribution())
		if err != nil {
			t.Fatal(err)
		}
		id, _ := entity.New(entity.KindHostConfig, selected.id)
		subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetCodex, target.ScopeProject, key)
		if err != nil {
			t.Fatal(err)
		}
		row, err := durable.NewManagedAggregateState(subject, value)
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, row)
	}
	state, err := fixture.before.WithManagedAggregates(rows)
	if err != nil {
		t.Fatal(err)
	}
	writeHostReleaseSnapshot(t, fixture, state)
	peerPath := filepath.Join(fixture.paths.ManifestRoot, ".codex", "istanbul", "config.toml")
	writeUnmanageFile(t, peerPath, []byte("model=true\n"))
	peer, err := mutation.ObservePersistedDirectoryEntryAuthority(peerPath)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(fixture.paths.ManifestRoot, ".codex", "İstanbul", "config.toml"), physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !selected.MatchesRetainedExact(peer.Exact()) {
		t.Skip("filesystem does not expose the provisional legacy comparison ambiguity")
	}
	address, err := ownership.NewManagedAddress(peer.Exact(), footprint)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, fixture.registry.Claims()[0].Owner())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry([]ownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, fixture.paths.OwnershipRegistryPath, content)
	request := fixture.request
	request.Mode = UnmanageModeDryRun
	if _, err := UnmanageHostConfig(t.Context(), request); err == nil {
		t.Fatal("claimless selected document admitted removal of another declaration's folded legacy claim")
	}
}
