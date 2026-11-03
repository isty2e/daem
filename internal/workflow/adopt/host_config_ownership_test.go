package adopt

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/executionidentity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/lock/nativefragment"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func seedCaptureWholeHistory(t *testing.T, fixture captureWorkflowFixture, scenario string) ([]byte, []byte) {
	t.Helper()
	environment, err := declarationmanifest.Decode(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	config := environment.HostConfigs()[0]
	placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
	if err != nil {
		t.Fatal(err)
	}
	id := config.ID()
	if scenario == "unrelated-declaration" {
		id, err = entity.New(entity.KindHostConfig, "other")
		if err != nil {
			t.Fatal(err)
		}
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, config.Target(), config.Scope())
	if err != nil {
		t.Fatal(err)
	}
	row, err := durable.NewManagedPathState(subject, []target.Target{config.Target()}, config.Scope(), placement.Destination(), artifact.HashFileContent([]byte("different recorded whole content")), realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 1, ManagedPaths: []durable.ManagedPathState{row}})
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.StatefilePath, state, 0o600); err != nil {
		t.Fatal(err)
	}
	statePath := fixture.paths.StatefilePath
	if scenario == "foreign" {
		statePath = filepath.Join(fixture.paths.ManifestRoot, "foreign-state")
		if err := os.WriteFile(statePath, []byte("other authority"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	persisted, err := mutation.ObservePersistedDirectoryEntryAuthority(statePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(persisted.Exact(), fixture.paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	host := fixture.host
	if scenario == "binding" {
		host = filepath.Join(fixture.paths.ManifestRoot, "other-host")
		if err := os.WriteFile(host, []byte("same namespace is not proven by bytes"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	observed, err := mutation.ObserveDirectoryEntryAuthority(host)
	if err != nil {
		t.Fatal(err)
	}
	physical, present := observed.Exact()
	if !present {
		t.Fatal("fixture host authority is missing")
	}
	address, err := ownership.NewManagedAddress(physical, output.WholePathFootprint())
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if scenario == "reserved" {
		claim, err = ownership.NewReservedClaim(address, owner, "capture-pending-operation")
	}
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(owner, placement.Destination(), physical)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := ownership.NewRegistry([]ownership.Claim{claim}, binding)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "protection" {
		document, err := ownership.NewProtectedDocument([]ownership.DocumentBinding{binding}, 0, 0, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		invocation, err := executionidentity.NewInvocation(strings.Repeat("a", 32))
		if err != nil {
			t.Fatal(err)
		}
		correlation, err := ownership.NewProtectionCorrelation("capture-operation", strings.Repeat("a", 64), strings.Repeat("b", 64))
		if err != nil {
			t.Fatal(err)
		}
		protection, err := ownership.NewArmedProtection(owner, invocation, correlation, []ownership.ProtectedDocument{document})
		if err != nil {
			t.Fatal(err)
		}
		registry, err = ownership.NewRegistryWithProtections([]ownership.Claim{claim}, []ownership.DocumentBinding{binding}, []ownership.PendingProtection{protection})
		if err != nil {
			t.Fatal(err)
		}
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.paths.OwnershipRegistryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.OwnershipRegistryPath, registryBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return state, registryBytes
}

func TestHostConfigCaptureAdmitsOwnRecordedWholeNarrowingAndPreservesMetadata(t *testing.T) {
	host := []byte(`{"nested":{"a":42},"unselected":8}`)
	fixture := newCaptureWorkflowFixture(t, "keys", host)
	state, registry := seedCaptureWholeHistory(t, fixture, "same-declaration")
	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen", Keys: []output.KeyPath{captureKey(t, "/nested")}})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if !plan.NarrowingHandoff() || !plan.ReplacesDesiredValues() {
		t.Fatal("recorded whole handoff was hidden from approval")
	}
	if _, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err != nil {
		t.Fatal(err)
	}
	assertCaptureArtifact(t, fixture.host, host)
	assertCaptureArtifact(t, fixture.paths.StatefilePath, state)
	assertCaptureArtifact(t, fixture.paths.OwnershipRegistryPath, registry)
	if bytes.Equal(plan.SourceBefore(), plan.SourceAfter()) {
		t.Fatal("capture failed to replace desired source values")
	}
}

func TestHostConfigCaptureRefusesForeignReservedOrUnrelatedWholeHistory(t *testing.T) {
	for _, scenario := range []string{"foreign", "reserved", "unrelated-declaration", "binding", "protection"} {
		t.Run(scenario, func(t *testing.T) {
			fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":42}}`))
			state, registry := seedCaptureWholeHistory(t, fixture, scenario)
			source, _ := os.ReadFile(fixture.source)
			if plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"}); err == nil || plan != nil {
				if plan != nil {
					_ = plan.Close()
				}
				t.Fatal("capture borrowed unsupported ownership")
			}
			assertCaptureArtifact(t, fixture.source, source)
			assertCaptureArtifact(t, fixture.paths.StatefilePath, state)
			assertCaptureArtifact(t, fixture.paths.OwnershipRegistryPath, registry)
		})
	}
}

func TestHostConfigCaptureDoesNotRequireUnselectedAliasMutationCohort(t *testing.T) {
	fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":42},"peer":77}`))
	peerPath := filepath.Join(filepath.Dir(fixture.host), "unselected.json")
	if err := os.Remove(peerPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("capture.jsonc", peerPath); err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	var rows []durable.ManagedAggregateState
	var destinations []output.Destination
	for index, config := range environment.HostConfigs() {
		content := []byte(`{"nested":{"a":1}}`)
		if index == 1 {
			content = []byte(`{"peer":1}`)
		}
		contributions, err := nativefragment.HostConfigKeyContributions(t.Context(), config, content, nativeconfig.DocumentLimits())
		if err != nil {
			t.Fatal(err)
		}
		for _, contribution := range contributions {
			row, err := durable.NewManagedAggregateState(contribution.SubjectID(), contribution.Contribution())
			if err != nil {
				t.Fatal(err)
			}
			rows = append(rows, row)
			destinations = append(destinations, row.Contribution().AggregateRoot())
		}
	}
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 1, ManagedAggregates: rows})
	if err != nil {
		t.Fatal(err)
	}
	state, err := statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(fixture.paths.StateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.StatefilePath, state, 0o600); err != nil {
		t.Fatal(err)
	}
	persisted, err := mutation.ObservePersistedDirectoryEntryAuthority(fixture.paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(persisted.Exact(), fixture.paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	observed, err := mutation.ObserveDirectoryEntryAuthority(fixture.host)
	if err != nil {
		t.Fatal(err)
	}
	physical, _ := observed.Exact()
	var claims []ownership.Claim
	var bindings []ownership.DocumentBinding
	for index, row := range rows {
		address, err := ownership.NewManagedAddress(physical, row.Contribution().Contract().Address().Footprint())
		if err != nil {
			t.Fatal(err)
		}
		claim, err := ownership.NewActiveClaim(address, owner)
		if err != nil {
			t.Fatal(err)
		}
		claims = append(claims, claim)
		binding, err := ownership.NewDocumentBinding(owner, destinations[index], physical)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	registry, err := ownership.NewRegistry(claims, bindings...)
	if err != nil {
		t.Fatal(err)
	}
	registryBytes, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(fixture.paths.OwnershipRegistryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.OwnershipRegistryPath, registryBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen", Keys: []output.KeyPath{captureKey(t, "/nested")}})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	if _, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err != nil {
		t.Fatal(err)
	}
	assertCaptureArtifact(t, fixture.paths.StatefilePath, state)
	assertCaptureArtifact(t, fixture.paths.OwnershipRegistryPath, registryBytes)
	assertCaptureArtifact(t, fixture.host, []byte(`{"nested":{"a":42},"peer":77}`))
}
