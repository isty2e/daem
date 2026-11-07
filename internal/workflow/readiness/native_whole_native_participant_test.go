package readiness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	"github.com/isty2e/daem/internal/output"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestNativeWholeProducerRetainsOtherNativeDeclarationAndAlias(t *testing.T) {
	for _, separateAlias := range []bool{false, true} {
		t.Run(map[bool]string{false: "declaration", true: "physical alias"}[separateAlias], func(t *testing.T) {
			fixture := newNativePlanningFixture(t, target.ScopeProject, "extension-settings/test.json")
			before := fixture.observation.Before
			old := before.ManagedAggregates()[0]
			destination := old.Contribution().AggregateRoot()
			if separateAlias {
				destination = outputtest.Parse(t, "extension-settings/other.json")
				path := filepath.Join(namespacetest.LegacyRoot(t, fixture.observation.Roots, output.RootProject).SelectedPath, destination.RelativePath())
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(fixture.hostPath, path); err != nil {
					t.Fatal(err)
				}
			}
			contract, err := aggregate.NewHostConfigProjectionContract(target.TargetPi, target.ScopeProject, destination, nativePlanningKey(t, "z"), old.Contribution().CodecContractID())
			if err != nil {
				t.Fatal(err)
			}
			id, err := entity.New(entity.KindHostConfig, "peer")
			if err != nil {
				t.Fatal(err)
			}
			subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeProject, contract.Address().KeyPath())
			if err != nil {
				t.Fatal(err)
			}
			body, err := aggregate.NewManagedContribution(contract, old.Contribution().CanonicalContribution())
			if err != nil {
				t.Fatal(err)
			}
			row, err := durable.NewManagedAggregateState(subject, body)
			if err != nil {
				t.Fatal(err)
			}
			before, err = before.WithManagedAggregates(append(before.ManagedAggregates(), row))
			if err != nil {
				t.Fatal(err)
			}
			if separateAlias {
				fixture.observation.Aliases = append(fixture.observation.Aliases, contract.Address().Document())
			}
			fixture.persistBefore(t, before, nil)
			plan, err := PrepareNativeWholeAdoption(t.Context(), fixture.adoption(t, true))
			if err != nil || !plan.Decision().IsBlocked() || len(plan.RetiredSubjects()) != 0 {
				t.Fatal("whole adoption consumed another declaration or alias", err)
			}
		})
	}
}
