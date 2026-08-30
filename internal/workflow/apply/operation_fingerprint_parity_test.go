package apply

import (
	"fmt"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
	"github.com/isty2e/daem/internal/reconcile"
)

func TestCompiledApplyFingerprintMatchesLegacyProjection(t *testing.T) {
	t.Parallel()

	planned := applyAuthorityTestPlan(t)
	for _, operationContext := range []reconcile.OperationContext{
		reconcile.ContextApply,
		reconcile.ContextDryRun,
	} {
		compiled, err := applyOperationFingerprint(planned, operationContext)
		if err != nil {
			t.Fatal(err)
		}
		legacy, err := legacyApplyOperationFingerprint(planned, operationContext)
		if err != nil {
			t.Fatal(err)
		}
		if !compiled.Equal(legacy) {
			t.Fatalf("compiled %s fingerprint differs from the legacy projection", operationContext)
		}
	}
}

type legacyApplyFingerprintFacts struct {
	ManifestPath        string
	LockfilePath        string
	LockfileExplicit    bool
	Targets             []string
	ManageUnmanaged     bool
	DelegateMode        reconcile.OperationContext
	ManagedPaths        []managedPathFingerprintFacts
	Aggregates          []aggregateFingerprintFacts
	MCPProviders        []mcpProviderFingerprintFacts
	RelationActions     []relationFingerprintFacts
	RelationOrders      []relationOrderFingerprintFacts
	CarrierAdoptions    []carrierAdoptionFingerprintFacts
	CarrierAbsences     []carrierAbsenceFingerprintFacts
	DelegateActions     []delegateFingerprintFacts
	Owner               ownershipOwnerFingerprintFacts
	Ownership           []ownershipObservationFingerprintFacts
	GlobalCarrierClaims []carrierClaimFingerprintFacts
	Diagnostics         []diagnosticFingerprintFacts
	ProjectRoot         *projectRootFingerprintFacts
}

func legacyApplyOperationFingerprint(
	planned commandPlan,
	operationContext reconcile.OperationContext,
) (mutation.OperationFingerprint, error) {
	projectRoot, err := projectRootFingerprint(planned)
	if err != nil {
		return mutation.OperationFingerprint{}, err
	}
	result := planned.result
	relations := relationFingerprintRows(planned.assessment.Reconciliation.Relations())
	relationOrders := relationOrderFingerprintRows(planned.assessment.Reconciliation.RelationOrders())
	carrierAbsences := carrierAbsenceFingerprintRows(planned.assessment.Reconciliation.CarrierAbsences())
	carrierAdoptions := make(
		[]carrierAdoptionFingerprintFacts,
		0,
		len(planned.assessment.Reconciliation.CarrierAdoptions()),
	)
	for _, action := range planned.assessment.Reconciliation.CarrierAdoptions() {
		carrierAdoptions = append(carrierAdoptions, carrierAdoptionFingerprintFacts{
			Subject:      action.Subject(),
			Target:       string(action.Target()),
			Scope:        string(action.Scope()),
			Result:       action.Result(),
			Blocker:      action.Lifecycle().Blocker(),
			PlanIdentity: action.PlanIdentity(),
		})
	}
	delegates := delegateFingerprintRows(result.Reconciliation.Delegates())
	targets := planned.context.Selection.Targets()
	targetValues := make([]string, 0, len(targets))
	for _, selected := range targets {
		targetValues = append(targetValues, string(selected))
	}
	fingerprint, err := operationplan.HashJSON(legacyApplyFingerprintFacts{
		ManifestPath:     result.ManifestPath,
		LockfilePath:     result.LockfilePath,
		LockfileExplicit: result.LockfileExplicit,
		Targets:          targetValues,
		ManageUnmanaged:  planned.context.ManageUnmanagedMatches,
		DelegateMode:     operationContext,
		ManagedPaths:     managedPathFingerprintRows(planned.assessment.Reconciliation.ManagedPaths()),
		Aggregates:       aggregateFingerprintRows(planned.assessment.Reconciliation.Aggregates()),
		MCPProviders:     mcpProviderFingerprintRows(planned.assessment.MCPProviders),
		RelationActions:  relations,
		RelationOrders:   relationOrders,
		CarrierAdoptions: carrierAdoptions,
		CarrierAbsences:  carrierAbsences,
		DelegateActions:  delegates,
		Owner: ownershipOwnerFingerprintFacts{
			StatefileAuthority: pathAuthorityFingerprintFactsFor(
				planned.assessment.Owner.StatefileAuthority(),
			),
			ManifestPath: planned.assessment.Owner.ManifestPath(),
		},
		Ownership:           ownershipFingerprintFacts(planned.assessment.Ownership),
		GlobalCarrierClaims: carrierClaimFingerprintRows(planned.assessment.GlobalCarrierClaims),
		Diagnostics:         diagnosticFingerprintRows(result.Diagnostics),
		ProjectRoot:         projectRoot,
	})
	if err != nil {
		return mutation.OperationFingerprint{}, fmt.Errorf("fingerprint apply plan: %w", err)
	}
	return fingerprint, nil
}
