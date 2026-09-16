package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lock"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func carrierFixtureClaim(t *testing.T, owner stateauthority.Authority, name string, scope target.Scope) carrier.ManagedCarrierClaim {
	t.Helper()
	source, err := desiredextension.NewSourceRef(desiredextension.SourceKindMarketplace, name+"@official")
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{Name: name, Carrier: desiredextension.CarrierClaudeCodePlugin, Target: target.TargetClaudeCode, Scope: scope, Source: source})
	if err != nil {
		t.Fatal(err)
	}
	relation, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := hostrelation.NewSubjectKey(name + "@official")
	if err != nil {
		t.Fatal(err)
	}
	contract, err := lock.NewDelegatedRelationCarrierContract(value.ID(), value.CarrierKey(), relation, subject)
	if err != nil {
		t.Fatal(err)
	}
	identity, admitted, err := carrier.ManagedCarrierIdentityFromLockedRecord(contract)
	if err != nil || !admitted {
		t.Fatalf("carrier identity: %t, %v", admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(contract, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := carrier.NewManagedCarrierClaim(owner, identity, request, carrier.ClaimProvenanceExplicitlyAdoptedObserved)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func outputClaim(t *testing.T, owner stateauthority.Authority, path string) ownership.Claim {
	t.Helper()
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(observed.Exact(), "")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := ownership.NewActiveClaim(address, owner)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func seedClaims(t *testing.T, paths, legacy daempaths.Paths) (ownership.Claim, carrier.ManagedCarrierClaim, string) {
	t.Helper()
	from, err := authorityFor(legacy)
	if err != nil {
		t.Fatal(err)
	}
	foreignRoot := t.TempDir()
	foreignPath, err := mutation.ObservePersistedDirectoryEntryAuthority(filepath.Join(foreignRoot, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := stateauthority.New(foreignPath.Exact(), filepath.Join(foreignRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(os.Getenv("HOME"), "installed.txt")
	writeFixture(t, installed, []byte("installed payload\n"))
	if err := os.Chmod(installed, 0o640); err != nil {
		t.Fatal(err)
	}
	foreignOutput := outputClaim(t, foreign, filepath.Join(foreignRoot, "output"))
	outputs, err := ownership.NewRegistry([]ownership.Claim{outputClaim(t, from, installed), foreignOutput})
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(outputs)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, paths.OwnershipRegistryPath, content)
	selectedCarrier := carrierFixtureClaim(t, from, "selected", target.ScopeGlobal)
	foreignCarrier := carrierFixtureClaim(t, foreign, "foreign", target.ScopeGlobal)
	carriers, err := carrier.NewGlobalCarrierClaims([]carrier.ManagedCarrierClaim{selectedCarrier, foreignCarrier})
	if err != nil {
		t.Fatal(err)
	}
	content, err = carrierclaim.Marshal(carriers)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, paths.CarrierClaimRegistryPath, content)
	projectCarrier := carrierFixtureClaim(t, from, "project-selected", target.ScopeProject)
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedCarrierClaims: []carrier.ManagedCarrierClaim{projectCarrier}})
	if err != nil {
		t.Fatal(err)
	}
	content, err = statefile.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacy.StatefilePath, content)
	return foreignOutput, foreignCarrier, installed
}

func keepForeignOutputOnly(t *testing.T, paths daempaths.Paths, foreign ownership.Claim) {
	t.Helper()
	registry, err := ownership.NewRegistry([]ownership.Claim{foreign})
	if err != nil {
		t.Fatal(err)
	}
	content, err := ownershipstore.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, paths.OwnershipRegistryPath, content)
}

func TestStateMigrationWithRegistryOnlyLegacyManagement(t *testing.T) {
	paths, legacy := migrationFixture(t)
	foreign, _, _ := seedClaims(t, paths, legacy)
	keepForeignOutputOnly(t, paths, foreign)
	if err := os.Remove(legacy.StatefilePath); err != nil {
		t.Fatal(err)
	}
	if err := recoverygate.RequireClear(t.Context(), paths); err == nil {
		t.Fatal("registry-only legacy management was ignored")
	}
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if prepared.Disclosure().OutputClaims != 0 || prepared.Disclosure().CarrierClaims != 1 {
		t.Fatalf("disclosure = %#v", prepared.Disclosure())
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := recoverygate.RequireClear(t.Context(), paths); err != nil {
		t.Fatal(err)
	}
}

func TestStateMigrationTransfersBothRegistriesAndPreservesOtherOwners(t *testing.T) {
	paths, legacy := migrationFixture(t)
	foreignOutput, foreignCarrier, installed := seedClaims(t, paths, legacy)
	before, err := os.Lstat(installed)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if prepared.Disclosure().OutputClaims != 1 || prepared.Disclosure().CarrierClaims != 1 {
		t.Fatalf("disclosure = %#v", prepared.Disclosure())
	}
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	to, err := authorityFor(paths)
	if err != nil {
		t.Fatal(err)
	}
	store, err := ownershipstore.New(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if claim, ok := outputs.Exact(foreignOutput.Address()); !ok || !claim.Equal(foreignOutput) {
		t.Fatal("foreign output claim changed")
	}
	selectedAddress := outputClaim(t, to, installed).Address()
	if claim, ok := outputs.Exact(selectedAddress); !ok || !claim.OwnedBy(to) {
		t.Fatal("output authority did not transfer")
	}
	carrierStore, err := carrierclaim.New(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	carriers, err := carrierStore.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	foreignFound, selectedFound := false, false
	for _, claim := range carriers.Claims() {
		foreignFound = foreignFound || claim.ExactEqual(foreignCarrier)
		selectedFound = selectedFound || (claim.Owner().Equal(to) && claim.Provenance() == carrier.ClaimProvenanceExplicitlyAdoptedObserved)
	}
	if !foreignFound || !selectedFound {
		t.Fatal("carrier facts did not transfer exactly")
	}
	snapshot, err := statefile.Load(t.Context(), paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ManagedCarrierClaims()) != 1 || !snapshot.ManagedCarrierClaims()[0].Owner().Equal(to) {
		t.Fatal("embedded owner did not transfer")
	}
	after, err := os.Lstat(installed)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(installed)
	if err != nil || !bytes.Equal(content, []byte("installed payload\n")) || !os.SameFile(before, after) || before.Mode() != after.Mode() {
		t.Fatalf("installed output changed: %v", err)
	}
}
