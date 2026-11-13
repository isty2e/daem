package authoring

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

type hostReleaseFixture struct {
	paths         daempaths.Paths
	host          string
	before        durable.Snapshot
	registry      ownership.Registry
	request       UnmanageHostConfigRequest
	contributions aggregate.ContributionCatalog
}

func newHostReleaseFixture(t *testing.T, claimless bool) hostReleaseFixture {
	t.Helper()
	root := t.TempDir()
	configureUnmanageTestHomes(t, root)
	paths := unmanageTestPaths(t, filepath.Join(root, "project"))
	writeUnmanageFile(t, paths.ManifestPath, []byte("version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='project'\npath='config.toml'\nsource='./missing-source.toml'\nownership='keys'\n"))
	id, err := entity.New(entity.KindHostConfig, "preferences")
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: target.TargetCodex, Scope: target.ScopeProject, Path: "config.toml", Ownership: hostconfig.OwnershipKeys})
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("model")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, placement.Destination(), footprint, "native-toml-v1")
	if err != nil {
		t.Fatal(err)
	}
	document, err := configdocument.Parse(t.Context(), []byte("model=true\n"), configdocument.TOML, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32})
	if err != nil {
		t.Fatal(err)
	}
	native, found, err := document.Lookup(key)
	if err != nil || !found {
		t.Fatal("fixture has no model value", err)
	}
	value, err := aggregate.NewManagedContribution(contract, native.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetCodex, target.ScopeProject, key)
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedAggregateState(subject, value)
	if err != nil {
		t.Fatal(err)
	}
	before, err := durable.NewSnapshot(durable.SnapshotInput{ManagedAggregates: []durable.ManagedAggregateState{row}, NativeDocumentCommit: 1})
	if err != nil {
		t.Fatal(err)
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	state, err := codec.Encode(before)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, paths.StatefilePath, state)
	host := filepath.Join(paths.ManifestRoot, ".codex", "config.toml")
	writeUnmanageFile(t, host, []byte("model=false\n# user drift\nunmanaged=1\n"))
	stateObservation, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(stateObservation.Exact(), paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(host)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(observed.Exact(), footprint)
	if err != nil {
		t.Fatal(err)
	}
	var claims []ownership.Claim
	var bindings []ownership.DocumentBinding
	if !claimless {
		claim, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
		binding, err := ownership.NewDocumentBinding(owner, placement.Destination(), observed.Exact())
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	} else {
		foreignObservation, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(root, "foreign-state.json"), physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
		if err != nil {
			t.Fatal(err)
		}
		foreignKey, present := foreignObservation.Exact()
		if !present {
			t.Fatal("fixture foreign State path is not exact")
		}
		foreignOwner, err := stateauthority.New(foreignKey, filepath.Join(root, "foreign.toml"))
		if err != nil {
			t.Fatal(err)
		}
		claim, err := ownership.NewReservedClaim(address, foreignOwner, "foreign-interrupted")
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
	}
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, paths.OwnershipRegistryPath, content)
	carrier, err := carrierclaim.Marshal(durablecarrier.EmptyGlobalCarrierClaims())
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, paths.CarrierClaimRegistryPath, carrier)
	return hostReleaseFixture{paths: paths, host: host, before: before, registry: registry, request: UnmanageHostConfigRequest{ManifestPath: paths.ManifestPath, ID: "preferences", Mode: UnmanageModeWrite}, contributions: codecs.Contributions()}
}

func TestHostConfigUnmanageReleasesMetadataAndPreservesHostDrift(t *testing.T) {
	for _, claimless := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "claimless-with-foreign-reservation"}[claimless], func(t *testing.T) {
			fixture := newHostReleaseFixture(t, claimless)
			hostBefore := readUnmanageFile(t, fixture.host)
			previewRequest := fixture.request
			previewRequest.Mode = UnmanageModeDryRun
			preview, err := UnmanageHostConfig(t.Context(), previewRequest)
			if err != nil || preview.ManagementStatus != UnmanageManagementStatusWouldRelease {
				t.Fatal("retained management preview failed", err)
			}
			if current, err := statefile.LoadOptional(t.Context(), fixture.paths.StatefilePath); err != nil || !current.Equal(fixture.before) {
				t.Fatal("dry-run changed retained state", err)
			}
			result, err := UnmanageHostConfig(t.Context(), fixture.request)
			if err != nil || result.ManagementStatus != UnmanageManagementStatusReleased {
				t.Fatal("metadata-only native release failed", err)
			}
			if after := readUnmanageFile(t, fixture.host); !bytes.Equal(after, hostBefore) {
				t.Fatal("unmanage changed drifted host bytes")
			}
			if state, err := statefile.LoadOptional(t.Context(), fixture.paths.StatefilePath); err != nil || len(state.ManagedAggregates()) != 0 || state.NativeDocumentCommit() != fixture.before.NativeDocumentCommit() {
				t.Fatal("unmanage did not release exactly retained membership", err)
			}
			store, err := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if claimless && !registry.Equal(fixture.registry) {
				t.Fatal("claimless release changed the foreign reservation")
			}
			if !claimless && (len(registry.Claims()) != 0 || len(registry.DocumentBindings()) != 0) {
				t.Fatal("own claims/binding survived release")
			}
			if _, err := os.Stat(fixture.paths.LockfilePath); !os.IsNotExist(err) {
				t.Fatal("release recreated an absent default lock", err)
			}
			if claimless {
				for path, version := range map[string]int{fixture.paths.StatefilePath: 10, fixture.paths.OwnershipRegistryPath: 4, fixture.paths.CarrierClaimRegistryPath: 2} {
					var envelope struct {
						Version int `json:"version"`
					}
					if err := json.Unmarshal(readUnmanageFile(t, path), &envelope); err != nil || envelope.Version != version {
						t.Fatal("conversion deferral changed a legacy physical encoding", version, envelope.Version, err)
					}
				}
			}
		})
	}
}

func writeHostReleaseSnapshot(t *testing.T, fixture hostReleaseFixture, snapshot durable.Snapshot) {
	t.Helper()
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	content, err := codec.Encode(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	writeUnmanageFile(t, fixture.paths.StatefilePath, content)
}

func TestHostConfigReleaseOwnerSelectionKeepsStateAndProvenanceCoupled(t *testing.T) {
	fixture := newHostReleaseFixture(t, false)
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(fixture.paths.StatefilePath, physicalwork.MaximumPhysicalPathDepth, physicalwork.NewCaptureBudget(nil))
	if err != nil {
		t.Fatal(err)
	}
	own := fixture.registry.Claims()[0].Owner()
	foreign := newHostReleaseFixture(t, false).registry.Claims()[0].Owner()
	wrongManifest, err := stateauthority.New(own.StatefileAuthority(), foreign.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	wrongState, err := stateauthority.New(foreign.StatefileAuthority(), own.ManifestPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name   string
		owner  stateauthority.Authority
		refuse bool
	}{
		{"own", own, false}, {"distinct-foreign", foreign, false}, {"shared-State", wrongManifest, true}, {"conflicting-State", wrongState, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			claim, err := ownership.NewReservedClaim(fixture.registry.Claims()[0].Address(), scenario.owner, "provenance-test")
			if err != nil {
				t.Fatal(err)
			}
			registry, err := ownership.NewRegistry([]ownership.Claim{claim})
			if err != nil {
				t.Fatal(err)
			}
			selected, err := selectHostConfigReleaseOwner(fixture.paths.ManifestPath, observation, registry)
			if scenario.refuse {
				if err == nil {
					t.Fatal("incompatible selected owner provenance was accepted")
				}
			} else if err != nil || !selected.ExactEqual(own) {
				t.Fatal("foreign metadata changed selected own authority", err)
			}
		})
	}
}

func TestHostConfigReleaseKeepsOtherMembersOfTheSameDocument(t *testing.T) {
	for _, authoringOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "managed-release", true: "declaration-only"}[authoringOnly], func(t *testing.T) {
			fixture := newHostReleaseFixture(t, false)
			key, err := output.NewKeyPath("peer")
			if err != nil {
				t.Fatal(err)
			}
			footprint, err := output.NewKeyFootprint(key)
			if err != nil {
				t.Fatal(err)
			}
			binding := fixture.registry.DocumentBindings()[0]
			contract, err := aggregate.NewHostConfigProjectionContract(target.TargetCodex, target.ScopeProject, binding.Destination(), footprint, "native-toml-v1")
			if err != nil {
				t.Fatal(err)
			}
			value, err := aggregate.NewManagedContribution(contract, fixture.before.ManagedAggregates()[0].Contribution().CanonicalContribution())
			if err != nil {
				t.Fatal(err)
			}
			peerID, _ := entity.New(entity.KindHostConfig, "peer")
			subject, err := topologyprojection.HostConfigKeySubject(peerID, target.TargetCodex, target.ScopeProject, key)
			if err != nil {
				t.Fatal(err)
			}
			row, err := durable.NewManagedAggregateState(subject, value)
			if err != nil {
				t.Fatal(err)
			}
			peerState, err := fixture.before.WithManagedAggregates([]durable.ManagedAggregateState{row})
			if err != nil {
				t.Fatal(err)
			}
			before := peerState
			if !authoringOnly {
				before, err = fixture.before.WithManagedAggregates(append(fixture.before.ManagedAggregates(), row))
				if err != nil {
					t.Fatal(err)
				}
			}
			writeHostReleaseSnapshot(t, fixture, before)
			address, err := ownership.NewManagedAddress(binding.PathAuthority(), footprint)
			if err != nil {
				t.Fatal(err)
			}
			claim, err := ownership.NewActiveClaim(address, binding.Owner())
			if err != nil {
				t.Fatal(err)
			}
			claims := []ownership.Claim{claim}
			if !authoringOnly {
				claims = append(claims, fixture.registry.Claims()...)
			}
			registry, err := ownership.NewRegistry(claims, binding)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := ownershipstore.Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			writeUnmanageFile(t, fixture.paths.OwnershipRegistryPath, encoded)
			host := readUnmanageFile(t, fixture.host)
			stateBytes := readUnmanageFile(t, fixture.paths.StatefilePath)

			result, err := UnmanageHostConfig(t.Context(), fixture.request)
			if err != nil {
				t.Fatal("shared-document release failed", err)
			}
			wantStatus := UnmanageManagementStatusReleased
			if authoringOnly {
				wantStatus = UnmanageManagementStatusNotPresent
			}
			if result.ManagementStatus != wantStatus {
				t.Fatal("shared-document release misclassified retained management")
			}
			after, err := statefile.LoadOptional(t.Context(), fixture.paths.StatefilePath)
			if err != nil || !after.Equal(peerState) {
				t.Fatal("release changed another declaration's Snapshot", err)
			}
			store, err := ownershipstore.New(fixture.paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := store.LoadProtectionRegistry(t.Context())
			expected, expectedErr := ownership.NewRegistry([]ownership.Claim{claim}, binding)
			if err != nil || expectedErr != nil || !actual.Equal(expected) {
				t.Fatal("release removed a surviving claim or shared native binding", err, expectedErr)
			}
			if !bytes.Equal(host, readUnmanageFile(t, fixture.host)) {
				t.Fatal("shared-document release changed host bytes")
			}
			if authoringOnly && (!bytes.Equal(stateBytes, readUnmanageFile(t, fixture.paths.StatefilePath)) || !bytes.Equal(encoded, readUnmanageFile(t, fixture.paths.OwnershipRegistryPath))) {
				t.Fatal("authoring-only removal rewrote unchanged management")
			}
		})
	}
}

func TestHostConfigAuthoringOnlyRefusesOrphanAndProtectedOwnMetadata(t *testing.T) {
	for _, scenario := range []string{"orphan-claim", "missing-State", "orphan-binding", "reserved-own", "pending-protection"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newHostReleaseFixture(t, false)
			claims := fixture.registry.Claims()
			bindings := fixture.registry.DocumentBindings()
			var protections []ownership.PendingProtection
			if scenario != "pending-protection" {
				writeHostReleaseSnapshot(t, fixture, durable.EmptySnapshot())
			}
			switch scenario {
			case "missing-State":
				if err := os.Remove(fixture.paths.StatefilePath); err != nil {
					t.Fatal(err)
				}
			case "orphan-binding":
				claims = nil
			case "reserved-own":
				claim, err := ownership.NewReservedClaim(claims[0].Address(), claims[0].Owner(), "unsettled-native")
				if err != nil {
					t.Fatal(err)
				}
				claims = []ownership.Claim{claim}
			case "pending-protection":
				document, err := ownership.NewProtectedDocument(bindings, 0, 0, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				invocation, err := executionidentity.NewInvocation(strings.Repeat("a", 32))
				if err != nil {
					t.Fatal(err)
				}
				correlation, err := ownership.NewProtectionCorrelation("native-protection", strings.Repeat("b", 64), strings.Repeat("c", 64))
				if err != nil {
					t.Fatal(err)
				}
				protection, err := ownership.NewArmedProtection(bindings[0].Owner(), invocation, correlation, []ownership.ProtectedDocument{document})
				if err != nil {
					t.Fatal(err)
				}
				protections = []ownership.PendingProtection{protection}
			}
			registry, err := ownership.NewRegistryWithProtections(claims, bindings, protections)
			if err != nil {
				t.Fatal(err)
			}
			content, err := ownershipstore.Marshal(registry)
			if err != nil {
				t.Fatal(err)
			}
			writeUnmanageFile(t, fixture.paths.OwnershipRegistryPath, content)
			manifest, host := readUnmanageFile(t, fixture.paths.ManifestPath), readUnmanageFile(t, fixture.host)
			state, stateErr := os.ReadFile(fixture.paths.StatefilePath)
			_, err = UnmanageHostConfig(t.Context(), fixture.request)
			if err == nil {
				t.Fatal("release bypassed orphan metadata or protection")
			}
			if scenario == "pending-protection" {
				var pending *ownership.ProtectionPendingError
				if !errors.As(err, &pending) {
					t.Fatal("release hid pending protection", err)
				}
			}
			if !bytes.Equal(manifest, readUnmanageFile(t, fixture.paths.ManifestPath)) || !bytes.Equal(host, readUnmanageFile(t, fixture.host)) || !bytes.Equal(content, readUnmanageFile(t, fixture.paths.OwnershipRegistryPath)) {
				t.Fatal("refused release changed selected or host data")
			}
			current, currentErr := os.ReadFile(fixture.paths.StatefilePath)
			if !bytes.Equal(state, current) || os.IsNotExist(stateErr) != os.IsNotExist(currentErr) {
				t.Fatal("refused release changed State")
			}
		})
	}
}

func TestHostConfigReleaseQualifiesDriftWithoutFollowingAnUnavailableHost(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		unapplied    bool
		notDirectory bool
	}{
		{"bound-dangling", false, false},
		{"unapplied-dangling", true, false},
		{"bound-non-directory", false, true},
		{"unapplied-non-directory", true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			fixture := newHostReleaseFixture(t, false)
			if scenario.unapplied {
				writeHostReleaseSnapshot(t, fixture, durable.EmptySnapshot())
				content, err := ownershipstore.Marshal(ownership.EmptyRegistry())
				if err != nil {
					t.Fatal(err)
				}
				writeUnmanageFile(t, fixture.paths.OwnershipRegistryPath, content)
			}
			if err := os.Remove(fixture.host); err != nil {
				t.Fatal(err)
			}
			missing := filepath.Join(t.TempDir(), "missing", "settings.toml")
			if scenario.notDirectory {
				if err := os.Remove(filepath.Dir(fixture.host)); err != nil {
					t.Fatal(err)
				}
				writeUnmanageFile(t, filepath.Dir(fixture.host), []byte("user non-directory replacement\n"))
			} else if err := os.Symlink(missing, fixture.host); err != nil {
				t.Fatal(err)
			}
			result, err := UnmanageHostConfig(t.Context(), fixture.request)
			if err != nil {
				t.Fatal("known binding or future namespace could not release metadata", err)
			}
			want := UnmanageManagementStatusReleased
			if scenario.unapplied {
				want = UnmanageManagementStatusNotPresent
			}
			if result.ManagementStatus != want {
				t.Fatal("unavailable host changed management classification")
			}
			if scenario.notDirectory {
				if actual := readUnmanageFile(t, filepath.Dir(fixture.host)); !bytes.Equal(actual, []byte("user non-directory replacement\n")) {
					t.Fatal("release changed the non-directory host prefix")
				}
			} else {
				if actual, err := os.Readlink(fixture.host); err != nil || actual != missing {
					t.Fatal("release changed a dangling host link", err)
				}
				if _, err := os.Stat(missing); !os.IsNotExist(err) {
					t.Fatal("release created a host referent", err)
				}
			}
		})
	}
}

func TestHostConfigReleaseDoesNotUseHostDriftToBypassMetadataGuards(t *testing.T) {
	for _, scenario := range []string{"unbound-management", "unbound-orphan", "blocked-manifest", "dangling-control"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newHostReleaseFixture(t, false)
			if err := os.Remove(fixture.host); err != nil {
				t.Fatal(err)
			}
			if scenario == "dangling-control" {
				if err := os.Symlink(filepath.Join(fixture.paths.RecoveryDir, "future", "journal.json"), fixture.host); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Remove(filepath.Dir(fixture.host)); err != nil {
					t.Fatal(err)
				}
				writeUnmanageFile(t, filepath.Dir(fixture.host), []byte("user blocker\n"))
				registry := ownership.EmptyRegistry()
				if scenario == "unbound-orphan" {
					writeHostReleaseSnapshot(t, fixture, durable.EmptySnapshot())
					var err error
					registry, err = ownership.NewRegistry(fixture.registry.Claims())
					if err != nil {
						t.Fatal(err)
					}
				}
				if scenario == "blocked-manifest" {
					writeHostReleaseSnapshot(t, fixture, durable.EmptySnapshot())
					if err := os.Rename(fixture.paths.ManifestPath, filepath.Dir(fixture.host)); err != nil {
						t.Fatal(err)
					}
					fixture.request.ManifestPath = filepath.Dir(fixture.host)
				}
				content, err := ownershipstore.Marshal(registry)
				if err != nil {
					t.Fatal(err)
				}
				writeUnmanageFile(t, fixture.paths.OwnershipRegistryPath, content)
			}
			manifest := readUnmanageFile(t, fixture.request.ManifestPath)
			state := readUnmanageFile(t, fixture.paths.StatefilePath)
			registry := readUnmanageFile(t, fixture.paths.OwnershipRegistryPath)
			if _, err := UnmanageHostConfig(t.Context(), fixture.request); err == nil {
				t.Fatal("host drift bypassed namespace, orphan, or metadata alias refusal")
			}
			if !bytes.Equal(manifest, readUnmanageFile(t, fixture.request.ManifestPath)) || !bytes.Equal(state, readUnmanageFile(t, fixture.paths.StatefilePath)) || !bytes.Equal(registry, readUnmanageFile(t, fixture.paths.OwnershipRegistryPath)) {
				t.Fatal("refused drift changed release data")
			}
		})
	}
}
