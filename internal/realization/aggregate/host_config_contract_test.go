package aggregate

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestHostConfigContractShapeDoesNotDecideNativePlacement(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	key, err := output.NewKeyPath("theme")
	if err != nil {
		t.Fatal(err)
	}
	footprint, err := output.NewKeyFootprint(key)
	if err != nil {
		t.Fatal(err)
	}

	for _, selected := range target.SupportedTargets() {
		for _, scope := range target.SupportedScopes() {
			t.Run(string(selected)+"/"+string(scope), func(t *testing.T) {
				path := "not-a-native-surface/settings.json"
				if scope == target.ScopeGlobal {
					path = "~/" + path
				}
				root, err := output.Parse(path)
				if err != nil {
					t.Fatal(err)
				}
				contract, err := NewHostConfigProjectionContract(selected, scope, root, footprint, "native-json-v1")
				if err != nil {
					t.Fatalf("canonical target/scope shape was refused: %v", err)
				}
				subject, err := topologyprojection.HostConfigKeySubject(id, selected, scope, key)
				if err != nil {
					t.Fatal(err)
				}
				if err := ValidateSubjectContract(subject, contract); err != nil {
					t.Fatalf("canonical subject shape was refused: %v", err)
				}
			})
		}
	}
}

func TestHostConfigSubjectContractRejectsIndependentAxisForgery(t *testing.T) {
	id, err := entity.New(entity.KindHostConfig, "review")
	if err != nil {
		t.Fatal(err)
	}
	key, _ := output.NewKeyPath("theme")
	footprint, _ := output.NewKeyFootprint(key)
	root, _ := output.Parse("@pi-agent/settings.json")
	contract, err := NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, footprint, "native-json-v1")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.HostConfigKeySubject(id, target.TargetPi, target.ScopeGlobal, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateSubjectContract(subject, contract); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*ProjectionContract){
		func(c *ProjectionContract) { c.cardinality = ContributionSharedSet },
		func(c *ProjectionContract) { c.siblingPreservation = PreserveSiblingsSemantic },
		func(c *ProjectionContract) { c.address.placementID = "other-placement" },
		func(c *ProjectionContract) { c.address.mergeUnit = "other-unit" },
		func(c *ProjectionContract) { c.comparedFields = []string{"other"} },
		func(c *ProjectionContract) {
			other, _ := output.NewKeyPath("other")
			c.address.footprint, _ = output.NewKeyFootprint(other)
		},
		func(c *ProjectionContract) { c.address.document.target = target.TargetCodex },
	}
	for index, mutate := range mutations {
		candidate := cloneProjectionContract(contract)
		mutate(&candidate)
		if err := ValidateSubjectContract(subject, candidate); err == nil {
			t.Fatalf("accepted independent axis forgery %d", index)
		}
	}
	for _, invalid := range []output.Footprint{{}, output.WholePathFootprint()} {
		if _, err := NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, invalid, "native-json-v1"); err == nil {
			t.Fatal("accepted non-key authority")
		}
	}
	if _, err := NewHostConfigProjectionContract(target.TargetPi, target.ScopeGlobal, root, footprint, "unknown"); err == nil {
		t.Fatal("accepted unknown native grammar")
	}
}
