package aggregate

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

type staticCatalogValidator struct {
	id    CodecContractID
	err   error
	calls int
}

func (validator *staticCatalogValidator) ContractID() CodecContractID { return validator.id }
func (validator *staticCatalogValidator) ValidateContributions(ContributionSet) error {
	validator.calls++
	return validator.err
}

func TestContributionCatalogUsesStaticOnlyAdmissionAndOwnsTheIndex(t *testing.T) {
	set := staticCatalogSet(t)
	want := errors.New("value validator refused")
	validator := &staticCatalogValidator{id: set.Contract().CodecContractID(), err: want}
	validators := []ContributionValidator{validator}
	catalog, err := NewContributionCatalog(validators)
	if err != nil {
		t.Fatal(err)
	}
	validators[0] = &staticCatalogValidator{id: "unregistered"}
	if err := catalog.ValidateContributionSet(set); !errors.Is(err, want) || validator.calls != 1 {
		t.Fatalf("static value admission was not dispatched exactly once: %v", err)
	}
	if port, found := catalog.Lookup(validator.id); !found || port != validator {
		t.Fatal("caller slice mutation changed the checked registration")
	}
	if port, found := catalog.Lookup("unregistered"); found || port != nil {
		t.Fatal("caller slice mutation introduced an unadmitted identity")
	}

	validator.id = "reported-drift"
	if _, found := catalog.Lookup(set.Contract().CodecContractID()); found {
		t.Fatal("reported identity drift retained static admission")
	}
	if err := catalog.ValidateContributionSet(set); err == nil || validator.calls != 1 {
		t.Fatal("identity drift invoked the wrong validator")
	}
}

func TestContributionCatalogRejectsInvalidRegistrationsAndSetsBeforeDispatch(t *testing.T) {
	var typedNil *staticCatalogValidator
	for _, validators := range [][]ContributionValidator{
		nil,
		{nil},
		{typedNil},
		{&staticCatalogValidator{}},
		{&staticCatalogValidator{id: "same"}, &staticCatalogValidator{id: "same"}},
	} {
		catalog, err := NewContributionCatalog(validators)
		if err == nil || len(catalog.byContract) != 0 {
			t.Fatal("invalid static registration published a partial catalog")
		}
	}
	set := staticCatalogSet(t)
	validator := &staticCatalogValidator{id: set.Contract().CodecContractID()}
	catalog, err := NewContributionCatalog([]ContributionValidator{validator})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := topology.NewSubjectID(topology.SubjectProjection, "foreign.namespace", "foreign")
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewSubjectContribution(foreign, set.Contributions()[0].Contribution())
	if err != nil {
		t.Fatal(err)
	}
	uncorrelated, err := NewContributionSet([]SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []ContributionSet{{}, uncorrelated} {
		if err := catalog.ValidateContributionSet(input); err == nil || validator.calls != 0 {
			t.Fatal("invalid set or subject correlation reached a concrete validator")
		}
	}
	if err := (ContributionCatalog{}).ValidateContributionSet(set); err == nil {
		t.Fatal("an unregistered set acquired admission from a zero catalog")
	}
}

func TestFullCatalogRetainsDocumentGuardWhileStaticViewDoesNotRequireIt(t *testing.T) {
	set := staticCatalogSet(t)
	want := errors.New("concrete value admission")
	codec := &codecCatalogTestCodec{contractID: set.Contract().CodecContractID(), contributionErr: want}
	catalog, err := NewCodecCatalog([]Codec{codec})
	if err != nil {
		t.Fatal(err)
	}
	if err := catalog.ValidateContributionSet(set); !errors.Is(err, want) {
		t.Fatal("full catalog did not preserve concrete value admission")
	}
	if err := catalog.Contributions().ValidateContributionSet(set); !errors.Is(err, want) {
		t.Fatal("static view changed concrete value admission")
	}
	codec.maximumBytes = -1
	if err := catalog.ValidateContributionSet(set); err == nil || errors.Is(err, want) {
		t.Fatal("full catalog bypassed its document admission guard")
	}
	if err := catalog.Contributions().ValidateContributionSet(set); !errors.Is(err, want) {
		t.Fatal("static value admission required an irrelevant document budget")
	}
}

func TestStaticOnlyRegistrationCannotAcquireDocumentOperations(t *testing.T) {
	set := staticCatalogSet(t)
	static, err := NewContributionCatalog([]ContributionValidator{
		&staticCatalogValidator{id: set.Contract().CodecContractID()},
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid := CodecCatalog{contributions: static}
	if codec, found := invalid.Lookup(set.Contract().CodecContractID()); found || codec != nil {
		t.Fatal("a static-only registration acquired document capability")
	}
	if err := invalid.ValidateContributionSet(set); err == nil {
		t.Fatal("full catalog validation bypassed its operation capability")
	}
}

func staticCatalogSet(t testing.TB) ContributionSet {
	t.Helper()
	placement, found := HookPlacementFor(target.TargetCodex, target.ScopeProject)
	if !found {
		t.Fatal("Hook fixture placement unavailable")
	}
	body, err := placement.Contribution("structurally-valid-value")
	if err != nil {
		t.Fatal(err)
	}
	id, err := entity.New(entity.KindHook, "guard")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyhook.ProjectionSubjectID(id, target.TargetCodex, target.ScopeProject)
	if err != nil {
		t.Fatal(err)
	}
	item, err := NewSubjectContribution(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	set, err := NewContributionSet([]SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	return set
}
