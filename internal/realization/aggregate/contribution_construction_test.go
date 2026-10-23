package aggregate

import "testing"

func TestContributionConstructionKeepsStaticContractSeparateFromDesiredContent(t *testing.T) {
	original := mustManagedContribution(t, testSharedHookContributionInput(t, `{"command":"before"}`))
	contract := original.Contract()
	desired := "{\"command\":\"after ☃\"}\n"

	updated, err := NewManagedContribution(contract, desired)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Contract().Equal(contract) || updated.CanonicalContribution() != desired {
		t.Fatal("construction changed the static contract or desired bytes")
	}
	if original.Equal(updated) || original.CanonicalContribution() != `{"command":"before"}` {
		t.Fatal("changed content did not remain separate from the original contribution")
	}

	contract.comparedFields[0] = "mutated"
	if updated.ComparedFields()[0] != "command" || original.ComparedFields()[0] != "command" {
		t.Fatal("constructor retained mutable contract storage")
	}
}

func TestContributionConstructionRejectsInvalidContractAndPayload(t *testing.T) {
	valid := mustManagedContribution(t, testSharedHookContributionInput(t, `{"command":"review"}`)).Contract()
	invalid := valid.Clone()
	invalid.cardinality = "unknown"

	for _, test := range []struct {
		name     string
		contract ProjectionContract
		content  string
	}{
		{name: "zero contract", content: `{}`},
		{name: "invalid contract", contract: invalid, content: `{}`},
		{name: "absent payload", contract: valid},
		{name: "invalid utf8", contract: valid, content: string([]byte{0xff})},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := NewManagedContribution(test.contract, test.content)
			if err == nil || got.Validate() == nil {
				t.Fatal("invalid construction produced an admitted contribution")
			}
		})
	}
}
