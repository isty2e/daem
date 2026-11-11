package statefile

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/target"
)

func TestStateImageRejectsForeignOwnerInEveryFamily(t *testing.T) {
	complete := testV9Snapshot(t)
	owner := complete.PendingCarrierInstalls()[0].Owner().StatefileAuthority()
	reference, err := pathauthority.NewReference(owner, filepath.Join(t.TempDir(), "SelectedState.json"))
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := pathauthority.NewReference(pathtest.Exact(filepath.Join(t.TempDir(), "foreign.json")), reference.Locator())
	if err != nil {
		t.Fatal(err)
	}
	globalIdentity, globalRequest := stateCarrierIdentity(t, "global-removal", "global-removal@official", target.ScopeGlobal)
	globalClaim, err := durablecarrier.NewManagedCarrierClaim(complete.ManagedCarrierClaims()[0].Owner(), globalIdentity, globalRequest, durablecarrier.ClaimProvenanceInstalledObserved)
	if err != nil {
		t.Fatal(err)
	}
	projectRemoval := complete.PendingCarrierRemovals()[0]
	globalRemoval, err := durablecarrier.NewPendingCarrierRemoval(globalClaim, projectRemoval.RemoveRequest(), projectRemoval.EffectPostconditions(), projectRemoval.EffectBaselines())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		input durable.SnapshotInput
	}{
		{name: "pending install", input: durable.SnapshotInput{PendingCarrierInstalls: complete.PendingCarrierInstalls()}},
		{name: "pending removal", input: durable.SnapshotInput{PendingCarrierRemovals: []durablecarrier.PendingCarrierRemoval{globalRemoval}}},
		{name: "managed claim", input: durable.SnapshotInput{ManagedCarrierClaims: complete.ManagedCarrierClaims()}},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := durable.NewSnapshot(test.input)
			if err != nil {
				t.Fatal(err)
			}
			image, err := NewStateImage(snapshot, reference)
			if err != nil || !image.Snapshot().Equal(snapshot) || !image.Reference().Equal(reference) {
				t.Fatal("matching statefile reference changed facts", err)
			}
			if _, err := NewStateImage(snapshot, foreign); err == nil {
				t.Fatal("foreign statefile identity was admitted")
			}
			content, err := MarshalImage(image)
			if err != nil {
				t.Fatal(err)
			}
			assertStateImageWireRejectsReference(t, content, foreign)
		})
	}
	if _, err := NewStateImage(durable.EmptySnapshot(), pathauthority.Reference{}); err == nil {
		t.Fatal("empty state invented its own reference")
	}
}

func TestStateImageDoesNotExposeMutableSnapshotStorage(t *testing.T) {
	snapshot := testV9Snapshot(t)
	reference, err := pathauthority.NewReference(snapshot.ManagedCarrierClaims()[0].Owner().StatefileAuthority(), filepath.Join(t.TempDir(), "SelectedState.json"))
	if err != nil {
		t.Fatal(err)
	}
	image, err := NewStateImage(snapshot, reference)
	if err != nil {
		t.Fatal(err)
	}
	claims := image.Snapshot().ManagedCarrierClaims()
	claims[0] = durablecarrier.ManagedCarrierClaim{}
	if !image.Snapshot().Equal(snapshot) || image.Validate() != nil {
		t.Fatal("snapshot access mutated a state image")
	}
}
