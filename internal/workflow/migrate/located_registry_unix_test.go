//go:build darwin || linux

package migrate

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

func TestStateRelocationRetainsLocatedRegistryEvidenceForForeignOwners(t *testing.T) {
	paths, legacy := migrationFixture(t)
	foreignOutput, foreignCarrier, installed := seedClaims(t, paths, legacy)
	sourceState, err := statefile.Load(t.Context(), legacy.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceObservation, err := mutation.ObservePersistedDirectoryEntryAuthority(legacy.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	sourceImage, err := statefile.NewStateImage(sourceState, sourceObservation.Reference())
	if err != nil {
		t.Fatal(err)
	}
	sourceBytes, err := statefile.MarshalImage(sourceImage)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, legacy.StatefilePath, sourceBytes)
	from, err := authorityFor(legacy)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "foreign-lookup")
	if err := os.Symlink(filepath.Dir(foreignCarrier.Owner().StatefileKey()), alias); err != nil {
		t.Fatal(err)
	}
	foreignReference, err := pathauthority.NewReference(foreignCarrier.Owner().StatefileAuthority(), filepath.Join(alias, filepath.Base(foreignCarrier.Owner().StatefileKey())))
	if err != nil {
		t.Fatal(err)
	}
	ownerEvidence := func(identities []pathauthority.Exact) []pathauthority.Reference {
		var references []pathauthority.Reference
		for _, identity := range identities {
			if identity.Equal(foreignReference.Identity()) {
				references = append(references, foreignReference)
				continue
			}
			observed, err := mutation.ObservePersistedDirectoryEntryAuthority(identity.Key())
			if err != nil || !observed.Exact().Equal(identity) {
				t.Fatal("fixture locator does not identify its source", err)
			}
			references = append(references, observed.Reference())
		}
		return references
	}
	content, err := os.ReadFile(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := ownershipstore.DecodeRegistryFacts(content)
	if err != nil {
		t.Fatal(err)
	}
	outputImage, err := ownershipstore.NewRegistryImage(outputs, ownerEvidence(ownershipstore.RequiredReferenceIdentities(outputs)))
	if err != nil {
		t.Fatal(err)
	}
	content, err = ownershipstore.MarshalImage(outputImage)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, paths.OwnershipRegistryPath, content)
	content, err = os.ReadFile(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	carriers, err := carrierclaim.DecodeRegistryFacts(content)
	if err != nil {
		t.Fatal(err)
	}
	carrierImage, err := carrierclaim.NewRegistryImage(carriers, ownerEvidence(carrierclaim.RequiredReferenceIdentities(carriers)))
	if err != nil {
		t.Fatal(err)
	}
	content, err = carrierclaim.MarshalImage(carrierImage)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, paths.CarrierClaimRegistryPath, content)
	beforeHost, err := os.Stat(installed)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Close()
	if _, err := prepared.Execute(t.Context()); err != nil {
		t.Fatal(err)
	}
	content, err = os.ReadFile(paths.OwnershipRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	outputAfter, err := ownershipstore.UnmarshalImage(content)
	if err != nil {
		t.Fatal("state relocation downgraded ownership evidence", err)
	}
	content, err = os.ReadFile(paths.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	carrierAfter, err := carrierclaim.UnmarshalImage(content)
	if err != nil {
		t.Fatal("state relocation downgraded carrier evidence", err)
	}
	to, err := authorityFor(paths)
	if err != nil {
		t.Fatal(err)
	}
	stateBytes, err := os.ReadFile(paths.StatefilePath)
	if err != nil {
		t.Fatal(err)
	}
	stateAfter, err := statefile.UnmarshalImage(stateBytes)
	if err != nil || !stateAfter.Reference().Identity().Equal(to.StatefileAuthority()) {
		t.Fatal("state relocation downgraded its image or retained the retired self reference", err)
	}
	for _, lookup := range []func(pathauthority.Exact) (pathauthority.Reference, bool){outputAfter.Reference, carrierAfter.Reference} {
		if retained, present := lookup(foreignReference.Identity()); !present || !retained.Equal(foreignReference) {
			t.Fatal("state relocation discarded foreign lookup spelling")
		}
		if _, present := lookup(from.StatefileAuthority()); present {
			t.Fatal("state relocation retained its retired source reference")
		}
		reference, present := lookup(to.StatefileAuthority())
		observed, err := mutation.ObservePersistedDirectoryEntryAuthority(paths.StatefilePath)
		if err != nil || !present || !reference.Equal(observed.Reference()) {
			t.Fatal("state relocation did not record the fresh destination reference", err)
		}
	}
	if claim, present := outputAfter.Registry().Exact(foreignOutput.Address()); !present || !claim.Equal(foreignOutput) {
		t.Fatal("state relocation changed foreign output facts")
	}
	foreignFound := false
	for _, claim := range carrierAfter.Registry().Claims() {
		foreignFound = foreignFound || claim.ExactEqual(foreignCarrier)
	}
	if !foreignFound {
		t.Fatal("state relocation changed foreign carrier facts")
	}
	afterHost, err := os.Stat(installed)
	content, readErr := os.ReadFile(installed)
	if err != nil || readErr != nil || !os.SameFile(beforeHost, afterHost) || beforeHost.Mode() != afterHost.Mode() || !bytes.Equal(content, []byte("installed payload\n")) {
		t.Fatal("metadata relocation changed the installed host", err, readErr)
	}
	repeat, err := PlanState(t.Context(), StateInput{ManifestPath: paths.ManifestPath})
	if err != nil {
		t.Fatal(err)
	}
	defer repeat.Close()
	if repeat.Disclosure().Action != "already_migrated" {
		t.Fatal("located registry relocation did not converge")
	}
}
