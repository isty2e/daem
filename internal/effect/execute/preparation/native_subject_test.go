package preparation

import (
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	nativecodec "github.com/isty2e/daem/internal/realization/aggregate/codec/native"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeProjectionEffectBindsContributionSubject(t *testing.T) {
	key, err := output.NewKeyPath("theme")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}
	codecID, _ := aggregate.HostConfigCodecForSyntax("json")
	contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, outputtest.Parse(t, ".pi/settings.json"), footprint, codecID)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := entity.New(entity.KindHostConfig, "settings")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(owner, target.TargetPi, target.ScopeProject, key)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contract})
	if err != nil {
		t.Fatal(err)
	}
	limits := configdocument.Limits{MaximumBytes: 1024, MaximumDepth: 16}
	codec, err := nativecodec.New(t.Context(), codecID, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, failure := codec.Read(aggregate.ExistingDocument([]byte(`{"theme":1}`)), selection)
	if failure != nil {
		t.Fatal(failure)
	}
	before := snapshot.States()[0]
	contribution, err := aggregate.NewManagedContribution(contract, before.CanonicalProjection())
	if err != nil {
		t.Fatal(err)
	}
	item, err := aggregate.NewSubjectContribution(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	desired, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
	if err != nil {
		t.Fatal(err)
	}
	previous, err := durable.NewManagedAggregateState(subject, contribution)
	if err != nil {
		t.Fatal(err)
	}
	absent, err := aggregate.NewProjectionState(contract, true, false, "")
	if err != nil {
		t.Fatal(err)
	}
	otherOwner, err := entity.New(entity.KindHostConfig, "unselected-settings")
	if err != nil {
		t.Fatal(err)
	}
	other, err := topologyprojection.HostConfigKeySubject(otherOwner, target.TargetPi, target.ScopeProject, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := aggregate.ValidateSubjectContract(other, contract); err != nil {
		t.Fatal("control subject must be independently valid at the same key", err)
	}

	for _, test := range []struct {
		name       string
		projection AggregateProjectionEffect
	}{
		{"desired", AggregateProjectionEffect{kind: AggregateEffectRecord, contract: contract, desired: &desired, before: before, expected: before}},
		{"previous", AggregateProjectionEffect{kind: AggregateEffectRemove, contract: contract, previous: []durable.ManagedAggregateState{previous}, before: before, expected: absent}},
		{"absent retirement", AggregateProjectionEffect{kind: AggregateEffectRecord, contract: contract, previous: []durable.ManagedAggregateState{previous}, before: absent, expected: absent}},
	} {
		t.Run(test.name, func(t *testing.T) {
			projection := test.projection
			subjectKind := projection.kind
			if !projection.before.Present() {
				subjectKind = AggregateEffectRemove
			}
			projection.subjects = []AggregateSubjectEffect{{kind: subjectKind, subject: subject, contract: contract}}
			if err := projection.validate(projection.before, projection.expected); err != nil {
				t.Fatal("actual contribution subject was refused", err)
			}
			if states := projection.PreviousStates(); len(states) != 0 {
				want := states[0]
				states[0] = durable.ManagedAggregateState{}
				if !projection.PreviousStates()[0].Equal(want) {
					t.Fatal("previous-state view overwrote the admitted transition")
				}
			}
			if !projection.before.Present() {
				for _, fault := range []string{"no prior", "no removals", "wrong kind", "duplicate removal"} {
					broken := projection
					broken.subjects = append([]AggregateSubjectEffect(nil), projection.subjects...)
					switch fault {
					case "no prior":
						broken.previous = nil
					case "no removals":
						broken.subjects = nil
					case "wrong kind":
						broken.subjects[0].kind = AggregateEffectRecord
					case "duplicate removal":
						broken.subjects = append(broken.subjects, broken.subjects[0])
					}
					if err := broken.validate(broken.before, broken.expected); err == nil {
						t.Fatalf("absent retirement accepted %s", fault)
					}
				}
			}
			projection.subjects[0].subject = other
			if err := projection.validate(projection.before, projection.expected); err == nil {
				t.Fatal("native effect accepted another entity's subject at the same physical key")
			}
		})
	}
}
