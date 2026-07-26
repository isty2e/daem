package lock

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationprofile "github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestDelegatedRelationCarrierContractCoversImplementedSpecs(t *testing.T) {
	cases := implementedCarrierContractCases()
	seenCarriers := make(map[desiredextension.Carrier]struct{}, len(cases))
	seenScopes := make(map[string]struct{}, len(cases))
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			carrier, subject, subjectKey, expected := mustCarrierFacts(t, test)
			entityID := mustContractEntityID(t, entity.KindExtension, test.declaration)
			contract, err := NewDelegatedRelationCarrierContract(entityID, carrier, subject, subjectKey)
			if err != nil {
				t.Fatalf("NewDelegatedRelationCarrierContract returned error: %v", err)
			}
			if contract.EntityID() != entityID || contract.SubjectID() != subject {
				t.Fatalf("contract identity = %q/%q", contract.EntityID(), contract.SubjectID())
			}
			if _, hasSupply := contract.ExactSupply(); hasSupply {
				t.Fatal("delegated relation contract exposed exact Supply")
			}
			realization, ok := contract.Realization()
			if !ok {
				t.Fatal("delegated relation contract is missing realization")
			}
			relation, ok := realization.DelegatedRelation()
			if !ok || relation.Target() != test.target || relation.Scope() != test.scope ||
				relation.SourceNamespace() != carrier.Source().String() ||
				!relation.ExpectedRelation().Equal(expected) {
				t.Fatalf("delegated relation = %#v", relation)
			}
			if err := relation.RouteRequest().Validate(); err != nil {
				t.Fatalf("route request: %v", err)
			}
			install, hasInstall := contract.OperationContract(OperationInstall)
			refresh, hasRefresh := contract.OperationContract(OperationRefresh)
			if !hasInstall || !hasRefresh {
				t.Fatalf("operation kinds = %v, want install and refresh", contract.OperationKinds())
			}
			if install.Route().RouteID != relation.RouteID() ||
				install.Route().AdapterContractVersion != relation.RouteContractVersion() {
				t.Fatalf("install route = %#v, realization route = %q/%q",
					install.Route(), relation.RouteID(), relation.RouteContractVersion())
			}
			if refresh.Route().RouteID == "" ||
				refresh.Route().AdapterContractVersion == "" ||
				refresh.Route() == install.Route() {
				t.Fatalf("refresh route = %#v, install route = %#v, want distinct durable routes",
					refresh.Route(), install.Route())
			}
			if refresh.Authority() != AuthorityNone || refresh.OrdinaryMutationEligible() {
				t.Fatalf("refresh authority/eligibility = %q/%t, want none/false",
					refresh.Authority(), refresh.OrdinaryMutationEligible())
			}
			removal, hasRemoval := contract.OperationContract(OperationRemove)
			wantRemoval := test.carrier == desiredextension.CarrierClaudeCodePlugin ||
				test.carrier == desiredextension.CarrierCodexPlugin ||
				test.carrier == desiredextension.CarrierOpenCodePlugin ||
				test.carrier == desiredextension.CarrierPiPackage ||
				test.carrier == desiredextension.CarrierAntigravityCLIPlugin
			if hasRemoval != wantRemoval {
				t.Fatalf("remove operation present = %t, want %t", hasRemoval, wantRemoval)
			}
			installRequest, err := DelegatedOperationRequest(contract, OperationInstall)
			if err != nil {
				t.Fatalf("derive install request: %v", err)
			}
			refreshRequest, err := DelegatedOperationRequest(contract, OperationRefresh)
			if err != nil {
				t.Fatalf("derive refresh request: %v", err)
			}
			if !installRequest.Equal(relation.RouteRequest()) {
				t.Fatalf("install request = %#v, want realization request %#v",
					installRequest, relation.RouteRequest())
			}
			currentRemoval, currentAdmitted, err := ResolveDelegatedCarrierRemoval(
				carrier,
				subject,
				expected,
				installRequest,
			)
			if err != nil || currentAdmitted != wantRemoval {
				t.Fatalf(
					"ResolveDelegatedCarrierRemoval = (%#v, %t, %v), want admitted=%t",
					currentRemoval,
					currentAdmitted,
					err,
					wantRemoval,
				)
			}
			if wantRemoval &&
				(!removal.OrdinaryMutationEligible() ||
					currentRemoval.Operation().Operation() != OperationRemove ||
					currentRemoval.Request().RouteID() != removal.Route().RouteID) {
				t.Fatalf("resolved removal = %#v", currentRemoval)
			}
			if refreshRequest.RouteID() != refresh.Route().RouteID ||
				refreshRequest.ContractVersion() != refresh.Route().AdapterContractVersion ||
				refreshRequest.CanonicalRequestHash() == installRequest.CanonicalRequestHash() {
				t.Fatalf("refresh request = %#v, install request = %#v, want exact distinct identity",
					refreshRequest, installRequest)
			}
			reconstructed, admitted, err := DelegatedRelationCarrier(contract)
			if err != nil || !admitted {
				t.Fatalf("reconstruct = %#v, %t, %v", reconstructed, admitted, err)
			}
			if reconstructed != test.carrier {
				t.Fatalf("reconstructed carrier = %q, want %q", reconstructed, test.carrier)
			}
			reconstructedKey, admitted, err := DelegatedRelationCarrierKey(contract)
			if err != nil || !admitted {
				t.Fatalf("reconstruct key = %#v, %t, %v", reconstructedKey, admitted, err)
			}
			if reconstructedKey != carrier {
				t.Fatalf("reconstructed carrier key = %#v, want %#v", reconstructedKey, carrier)
			}
			key := string(test.carrier) + "/" + string(test.scope)
			if _, duplicate := seenScopes[key]; duplicate {
				t.Fatalf("duplicate carrier/scope case %q", key)
			}
			seenScopes[key] = struct{}{}
			seenCarriers[test.carrier] = struct{}{}
		})
	}
	if len(seenCarriers) != len(implementedDelegatedRelationCarrierSpecs()) {
		t.Fatalf("covered carriers = %d, implemented specs = %d", len(seenCarriers), len(implementedDelegatedRelationCarrierSpecs()))
	}
	for _, spec := range implementedDelegatedRelationCarrierSpecs() {
		for _, scope := range spec.Profile.Carrier().AdmittedScopes() {
			key := string(spec.Profile.Carrier()) + "/" + string(scope)
			if _, covered := seenScopes[key]; !covered {
				t.Fatalf("carrier/scope %q has no lock-contract case", key)
			}
		}
	}
}

func TestDelegatedRelationCarrierSpecsFreezeRouteCapabilitiesAndNonClaims(t *testing.T) {
	type expectedCapabilities struct {
		scopes              []target.Scope
		hasObserver         bool
		hasRemoval          bool
		installVerification VerificationContract
		refreshVerification VerificationContract
		sourceExclusion     ReplayExclusionReason
	}
	expected := map[desiredextension.Carrier]expectedCapabilities{
		desiredextension.CarrierClaudeCodePlugin: {
			scopes:              []target.Scope{target.ScopeGlobal, target.ScopeProject},
			hasObserver:         true,
			hasRemoval:          true,
			installVerification: VerificationHostRelation,
			refreshVerification: VerificationHostRelation,
			sourceExclusion:     ReplayExclusionHostMarketplace,
		},
		desiredextension.CarrierCodexPlugin: {
			scopes:              []target.Scope{target.ScopeGlobal},
			hasObserver:         true,
			hasRemoval:          true,
			installVerification: VerificationHostRelation,
			refreshVerification: VerificationHostRelation,
			sourceExclusion:     ReplayExclusionHostMarketplace,
		},
		desiredextension.CarrierOpenCodePlugin: {
			scopes:              []target.Scope{target.ScopeGlobal, target.ScopeProject},
			hasObserver:         true,
			hasRemoval:          true,
			installVerification: VerificationHostRelation,
			refreshVerification: VerificationInsufficient,
			sourceExclusion:     ReplayExclusionHostSource,
		},
		desiredextension.CarrierPiPackage: {
			scopes:              []target.Scope{target.ScopeGlobal, target.ScopeProject},
			hasObserver:         true,
			hasRemoval:          true,
			installVerification: VerificationHostRelation,
			refreshVerification: VerificationInsufficient,
			sourceExclusion:     ReplayExclusionHostSource,
		},
		desiredextension.CarrierAntigravityCLIPlugin: {
			scopes:              []target.Scope{target.ScopeGlobal},
			hasRemoval:          true,
			installVerification: VerificationInsufficient,
			refreshVerification: VerificationInsufficient,
			sourceExclusion:     ReplayExclusionHostSource,
		},
	}

	specs := implementedDelegatedRelationCarrierSpecs()
	if len(specs) != len(expected) {
		t.Fatalf("delegated carrier specs = %d, expected capability rows = %d", len(specs), len(expected))
	}
	for _, spec := range specs {
		want, ok := expected[spec.Profile.Carrier()]
		if !ok {
			t.Fatalf("delegated carrier %q has no expected capability row", spec.Profile.Carrier())
		}
		t.Run(string(spec.Profile.Carrier()), func(t *testing.T) {
			if !slices.Equal(spec.Profile.Carrier().AdmittedScopes(), want.scopes) {
				t.Fatalf("allowed scopes = %v, want %v", spec.Profile.Carrier().AdmittedScopes(), want.scopes)
			}
			if spec.Ownership != OwnershipManifest || spec.OnAbsent != OnAbsentBlock {
				t.Fatalf("ownership/on-absent = %q/%q, want manifest/block", spec.Ownership, spec.OnAbsent)
			}
			expectedOperationCount := 2
			if want.hasObserver {
				expectedOperationCount++
			}
			if len(spec.OperationContracts) != expectedOperationCount {
				t.Fatalf(
					"operation contracts = %#v, want install, refresh, and observer=%t",
					spec.OperationContracts,
					want.hasObserver,
				)
			}

			install := operationContractInputForTest(t, spec.OperationContracts, OperationInstall)
			if install.Actuation != ActuationDelegatedHostRoute ||
				install.Authority != AuthorityManage ||
				install.EffectEnvelope != EffectEnvelopeIncomplete ||
				install.Idempotency != IdempotencyUnknown ||
				install.Verification != want.installVerification ||
				install.TrustActivation != TrustActivationUnknown ||
				install.Recovery != OperationRecoveryUnknown {
				t.Fatalf("install capability = %#v", install)
			}
			refresh := operationContractInputForTest(t, spec.OperationContracts, OperationRefresh)
			if refresh.Actuation != ActuationDelegatedHostRoute ||
				refresh.Authority != AuthorityNone ||
				refresh.EffectEnvelope != EffectEnvelopeIncomplete ||
				refresh.Idempotency != IdempotencyUnknown ||
				refresh.Verification != want.refreshVerification ||
				refresh.TrustActivation != TrustActivationUnknown ||
				refresh.Recovery != OperationRecoveryUnknown {
				t.Fatalf("refresh capability = %#v", refresh)
			}

			unsupported := []OperationKind{OperationPrune}
			if _, admitted := spec.Profile.OperationRoute(realizationprofile.OperationRemove); admitted != want.hasRemoval {
				t.Fatalf("removal profile = %t, want %t", admitted, want.hasRemoval)
			} else if !admitted {
				unsupported = append(unsupported, OperationRemove)
			}
			for _, unsupported := range unsupported {
				if _, ok := operationContractInputForTestOptional(spec.OperationContracts, unsupported); ok {
					t.Fatalf("unsupported operation %q is present", unsupported)
				}
			}

			observe, hasObserver := operationContractInputForTestOptional(spec.OperationContracts, OperationObserve)
			if hasObserver != want.hasObserver {
				t.Fatalf("observer capability = %t, want %t", hasObserver, want.hasObserver)
			}
			if hasObserver &&
				(observe.Actuation != ActuationNoMutation ||
					observe.Authority != AuthorityObserve ||
					observe.Verification != VerificationHostRelation ||
					observe.TrustActivation != TrustActivationNotRequired) {
				t.Fatalf("observe capability = %#v", observe)
			}

			replay := spec.Replay
			if replay.Invocation != ReplayPartial ||
				replay.Outcome != ReplayUnavailable ||
				replay.Derivation != ReplayNotApplicable {
				t.Fatalf("replay capability = %#v", replay)
			}
			for _, required := range []ReplayExclusionReason{
				ReplayExclusionHostSelectedArtifact,
				want.sourceExclusion,
				ReplayExclusionRuntimeDependency,
				ReplayExclusionHostApproval,
				ReplayExclusionRuntimeReadiness,
				ReplayExclusionToolInventory,
			} {
				if !replaySpecHasExclusion(replay, required) {
					t.Fatalf("replay exclusions = %#v, want reason %q", replay.Exclusions, required)
				}
			}
		})
	}
}
