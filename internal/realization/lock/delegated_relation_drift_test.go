package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/topology"
)

func TestDelegatedRelationCarrierReconstructionRejectsContractDrift(t *testing.T) {
	test := implementedCarrierContractCases()[1]
	carrier, subject, subjectKey, _ := mustCarrierFacts(t, test)
	contract, err := NewDelegatedRelationCarrierContract(
		mustContractEntityID(t, entity.KindExtension, test.declaration),
		carrier,
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	drifted := contract
	drifted.ownership = OwnershipAdopted
	if err := drifted.validate(); err != nil {
		t.Fatalf("drift fixture is not independently valid: %v", err)
	}
	_, admitted, err := DelegatedRelationCarrier(drifted)
	if !admitted || err == nil || !strings.Contains(err.Error(), "does not match the admitted carrier contract") {
		t.Fatalf("reconstruct drift = admitted %t, err %v", admitted, err)
	}

	exact := mustContractExactIdentity(t, "not-a-carrier")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	entityID := mustContractEntityID(t, entity.KindSkill, "not-a-carrier")
	supply, err := NewExactSupplySubjectContract(ExactSupplySubjectInput{
		EntityID:    entityID,
		SubjectID:   mustResourceSubjectID(t, entityID),
		ExactSupply: exact,
		Derivation:  direct,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, admitted, err := DelegatedRelationCarrier(supply); err != nil || admitted || got != "" {
		t.Fatalf("non-carrier reconstruct = %#v, %t, %v", got, admitted, err)
	}
}

func TestLockedSectionRejectsDelegatedCarrierRefinementDrift(t *testing.T) {
	test := implementedCarrierContractCases()[0]
	carrier, subject, subjectKey, _ := mustCarrierFacts(t, test)
	base, err := NewDelegatedRelationCarrierContract(
		mustContractEntityID(t, entity.KindExtension, test.declaration),
		carrier,
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	spec, _ := base.Realization()
	relation, _ := spec.DelegatedRelation()

	tests := []struct {
		name       string
		relation   realization.DelegatedRelation
		operations []OperationContract
		want       string
	}{
		{
			name:       "missing install operation",
			relation:   relation,
			operations: carrierOperationsExcept(base, OperationInstall),
			want:       "does not match the admitted carrier contract",
		},
		{
			name: "route id drift",
			relation: mustDelegatedRelationForCarrierTest(t, relation, delegatedCarrierRelationOverride{
				routeID: "other.route.install",
			}),
			want: "does not match the admitted carrier contract",
		},
		{
			name: "route contract drift",
			relation: mustDelegatedRelationForCarrierTest(t, relation, delegatedCarrierRelationOverride{
				routeContractVersion: "other-contract-v1",
			}),
			want: "does not match the admitted carrier contract",
		},
		{
			name: "canonical request hash drift",
			relation: mustDelegatedRelationForCarrierTest(t, relation, delegatedCarrierRelationOverride{
				canonicalRequestHash: "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			}),
			want: "does not match the admitted carrier contract",
		},
		{
			name: "managed instance key drift",
			relation: mustDelegatedRelationForCarrierTest(t, relation, delegatedCarrierRelationOverride{
				managedInstanceKey: `host-relation:v1:{"forged":true}`,
			}),
			want: "managed instance key does not match locked realization",
		},
		{
			name: "host visible key drift",
			relation: mustDelegatedRelationForCarrierTest(t, relation, delegatedCarrierRelationOverride{
				subjectKey: "other-context7",
			}),
			want: "does not match the admitted carrier contract",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operations := test.operations
			if operations == nil {
				operations = carrierOperationsForRelation(t, base, test.relation)
			}
			drifted := rebuildCarrierContractForTest(t, base, base.SubjectID(), test.relation, operations)

			_, err := NewLockedSection([]LockedSubjectContract{drifted}, nil)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("NewLockedSection error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLockedSectionRejectsUnknownDelegatedCarrierNamespace(t *testing.T) {
	test := implementedCarrierContractCases()[0]
	carrier, subject, subjectKey, _ := mustCarrierFacts(t, test)
	base, err := NewDelegatedRelationCarrierContract(
		mustContractEntityID(t, entity.KindExtension, test.declaration),
		carrier,
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	realization, _ := base.Realization()
	relation, _ := realization.DelegatedRelation()
	unknownSubject := mustTopologySubjectID(t, topology.SubjectHostRelation, "other.plugin-carrier", test.declaration)
	drifted := rebuildCarrierContractForTest(
		t,
		base,
		unknownSubject,
		relation,
		carrierOperationsForRelation(t, base, relation),
	)

	_, err = NewLockedSection([]LockedSubjectContract{drifted}, nil)
	if err == nil || !strings.Contains(err.Error(), "subject has no current topology refinement") {
		t.Fatalf("NewLockedSection error = %v, want unadmitted refinement diagnostic", err)
	}
}
