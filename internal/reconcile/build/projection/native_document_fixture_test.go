package projection

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func nativeOwnershipDocumentFixture(t *testing.T, currentText, destination string) (aggregate.DocumentObservation, []aggregate.ContributionSet) {
	t.Helper()
	root, err := output.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	codecID, _ := aggregate.HostConfigCodecForSyntax("json")
	limits := configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64}
	codec, err := nativecodec.New(t.Context(), codecID, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	var contracts []aggregate.ProjectionContract
	for _, name := range []string{"a", "z"} {
		key, err := output.NewKeyPath(name)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(key)
		if err != nil {
			t.Fatal(err)
		}
		contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, footprint, codecID)
		if err != nil {
			t.Fatal(err)
		}
		contracts = append(contracts, contract)
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	current, err := aggregate.ObserveDocument(codec, aggregate.ExistingDocument([]byte(currentText)), selection)
	if err != nil {
		t.Fatal(err)
	}
	want, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"a":2,"z":2}`)), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	var desired []aggregate.ContributionSet
	for _, state := range want.States() {
		body, err := aggregate.NewManagedContribution(state.Contract(), state.CanonicalProjection())
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, state.Contract().Address().KeyPath())
		if err != nil {
			t.Fatal(err)
		}
		item, err := aggregate.NewSubjectContribution(subject, body)
		if err != nil {
			t.Fatal(err)
		}
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		desired = append(desired, set)
	}
	return current, desired
}
