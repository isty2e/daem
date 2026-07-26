package lock

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestAntigravityLockOperationsFollowExactSourceClassAdmission(t *testing.T) {
	tests := []struct {
		name             string
		source           string
		wantObserve      bool
		wantRemove       bool
		wantVerification VerificationContract
	}{
		{
			name:             "marketplace selector",
			source:           "modern-web-guidance@google",
			wantObserve:      true,
			wantRemove:       true,
			wantVerification: VerificationHostRelation,
		},
		{
			name:             "local path",
			source:           "./plugins/guidance",
			wantVerification: VerificationInsufficient,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := carrierContractCase{
				name:        test.name,
				carrier:     desiredextension.CarrierAntigravityCLIPlugin,
				target:      target.TargetAntigravityCLI,
				scope:       target.ScopeGlobal,
				namespace:   "antigravity-cli.plugin-carrier",
				declaration: "guidance",
				sourceKind:  desiredextension.SourceKindHostSource,
				sourceRef:   test.source,
				subjectKey:  test.source,
			}
			carrier, subject, subjectKey, expected := mustCarrierFacts(t, fixture)
			contract, err := NewDelegatedRelationCarrierContract(
				mustContractEntityID(t, entity.KindExtension, fixture.declaration),
				carrier,
				subject,
				subjectKey,
			)
			if err != nil {
				t.Fatal(err)
			}
			install, ok := contract.OperationContract(OperationInstall)
			if !ok || install.Verification() != test.wantVerification {
				t.Fatalf(
					"install = %#v/%t, want verification %q",
					install,
					ok,
					test.wantVerification,
				)
			}
			if _, ok := contract.OperationContract(OperationObserve); ok != test.wantObserve {
				t.Fatalf("observe present = %t, want %t", ok, test.wantObserve)
			}
			if _, ok := contract.OperationContract(OperationRemove); ok != test.wantRemove {
				t.Fatalf("remove present = %t, want %t", ok, test.wantRemove)
			}
			_, admitted := mustResolveCurrentDelegatedRemoval(
				t,
				carrier,
				subject,
				expected,
				contract,
			)
			if admitted != test.wantRemove {
				t.Fatalf("removal admitted = %t, want %t", admitted, test.wantRemove)
			}
		})
	}
}

func TestOpenCodeRemovalContractUsesBoundedDirectProjection(t *testing.T) {
	fixture := carrierContractCase{
		name:        "OpenCode project plugin removal",
		carrier:     desiredextension.CarrierOpenCodePlugin,
		target:      target.TargetOpenCode,
		scope:       target.ScopeProject,
		namespace:   "opencode.plugin-carrier",
		declaration: "tools",
		sourceKind:  desiredextension.SourceKindHostSource,
		sourceRef:   "@acme/tools",
		subjectKey:  "@acme/tools",
	}
	carrier, subject, subjectKey, expected := mustCarrierFacts(t, fixture)
	contract, err := NewDelegatedRelationCarrierContract(
		mustContractEntityID(t, entity.KindExtension, fixture.declaration),
		carrier,
		subject,
		subjectKey,
	)
	if err != nil {
		t.Fatalf("NewDelegatedRelationCarrierContract: %v", err)
	}
	resolved, admitted := mustResolveCurrentDelegatedRemoval(
		t,
		carrier,
		subject,
		expected,
		contract,
	)
	if !admitted {
		t.Fatalf("ResolveDelegatedCarrierRemoval admitted = false")
	}
	operation := resolved.Operation()
	if operation.Actuation() != ActuationDirectProjection ||
		operation.Authority() != AuthorityRemove ||
		operation.Verification() != VerificationHostRelation ||
		operation.EffectEnvelope() != EffectEnvelopeComplete ||
		operation.Idempotency() != ConditionallyIdempotent ||
		operation.Recovery() != OperationRecoverySafeRetry ||
		operation.TrustActivation() != TrustActivationNotRequired ||
		!operation.OrdinaryMutationEligible() {
		t.Fatalf("OpenCode remove operation = %#v", operation)
	}
	if len(operation.EffectPostconditions().Requirements()) != 0 ||
		resolved.PreservesSharedCarrier() ||
		!slices.Contains(
			resolved.RemovedEffects(),
			"selected_opencode_plugin_config_relation",
		) ||
		!slices.Contains(
			resolved.RetainedEffects(),
			"package_manager_installations",
		) ||
		!slices.Contains(
			resolved.NonClaims(),
			"package_or_dependency_uninstall",
		) {
		t.Fatalf("OpenCode remove envelope = %#v", resolved)
	}
	if _, err := DelegatedOperationRequest(contract, OperationRemove); err == nil ||
		!strings.Contains(err.Error(), "does not use a delegated host route") {
		t.Fatalf("DelegatedOperationRequest(remove) error = %v", err)
	}
	if err := resolved.Request().Validate(); err != nil {
		t.Fatalf("direct removal request: %v", err)
	}
}

func TestPiPackageRemovalContractSeparatesSourceEffectsAndScopeTrust(t *testing.T) {
	tests := []struct {
		name               string
		scope              target.Scope
		source             string
		wantPostcondition  effectpostcondition.Requirement
		wantTrust          TrustActivationRequirement
		wantRemoved        string
		wantRetained       string
		wantAbsentNonClaim string
	}{
		{
			name:              "project npm",
			scope:             target.ScopeProject,
			source:            "npm:@acme/tools@1.2.3",
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantTrust:         TrustActivationRequired,
			wantRemoved:       "selected_scoped_npm_package_artifacts",
			wantRetained:      "npm_cache_and_logs",
		},
		{
			name:              "global git",
			scope:             target.ScopeGlobal,
			source:            "git:https://github.com/acme/tools.git@v1",
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantTrust:         TrustActivationNotRequired,
			wantRemoved:       "selected_scoped_git_checkout",
			wantRetained:      "unrelated_git_checkouts",
		},
		{
			name:               "project local",
			scope:              target.ScopeProject,
			source:             "./packages/tools",
			wantPostcondition:  effectpostcondition.LocalSourceUnchanged,
			wantTrust:          TrustActivationRequired,
			wantRemoved:        "selected_pi_package_relation",
			wantRetained:       "local_source_directory",
			wantAbsentNonClaim: "dependency_gc_beyond_native_remove",
		},
		{
			name:               "bare host path remains local",
			scope:              target.ScopeGlobal,
			source:             "github.com/acme/tools",
			wantPostcondition:  effectpostcondition.LocalSourceUnchanged,
			wantTrust:          TrustActivationNotRequired,
			wantRemoved:        "selected_pi_package_relation",
			wantRetained:       "local_source_directory",
			wantAbsentNonClaim: "dependency_gc_beyond_native_remove",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := carrierContractCase{
				name:        test.name,
				carrier:     desiredextension.CarrierPiPackage,
				target:      target.TargetPi,
				scope:       test.scope,
				namespace:   "pi.package-carrier",
				declaration: strings.ReplaceAll(test.name, " ", "-"),
				sourceKind:  desiredextension.SourceKindHostSource,
				sourceRef:   test.source,
				subjectKey:  test.source,
			}
			carrier, subject, subjectKey, expected := mustCarrierFacts(t, fixture)
			contract, err := NewDelegatedRelationCarrierContract(
				mustContractEntityID(t, entity.KindExtension, fixture.declaration),
				carrier,
				subject,
				subjectKey,
			)
			if err != nil {
				t.Fatalf("NewDelegatedRelationCarrierContract: %v", err)
			}
			resolved, admitted := mustResolveCurrentDelegatedRemoval(
				t,
				carrier,
				subject,
				expected,
				contract,
			)
			if !admitted {
				t.Fatalf("ResolveDelegatedCarrierRemoval admitted = false")
			}
			operation := resolved.Operation()
			if operation.TrustActivation() != test.wantTrust ||
				operation.Authority() != AuthorityRemove ||
				operation.EffectEnvelope() != EffectEnvelopeComplete ||
				operation.Idempotency() != ConditionallyIdempotent ||
				operation.Verification() != VerificationHostRelation ||
				operation.Recovery() != OperationRecoverySafeRetry ||
				!operation.OrdinaryMutationEligible() {
				t.Fatalf("remove operation = %#v", operation)
			}
			requirements := operation.EffectPostconditions().Requirements()
			if len(requirements) != 1 || requirements[0] != test.wantPostcondition {
				t.Fatalf(
					"effect postconditions = %#v, want %q",
					requirements,
					test.wantPostcondition,
				)
			}
			if !slices.Contains(resolved.RemovedEffects(), test.wantRemoved) {
				t.Fatalf("removed effects = %#v, want %q", resolved.RemovedEffects(), test.wantRemoved)
			}
			if !slices.Contains(resolved.RetainedEffects(), test.wantRetained) {
				t.Fatalf("retained effects = %#v, want %q", resolved.RetainedEffects(), test.wantRetained)
			}
			if test.wantAbsentNonClaim != "" &&
				slices.Contains(resolved.NonClaims(), test.wantAbsentNonClaim) {
				t.Fatalf("non-claims = %#v, did not want %q", resolved.NonClaims(), test.wantAbsentNonClaim)
			}
			removed := resolved.RemovedEffects()
			removed[0] = "forged"
			if slices.Contains(resolved.RemovedEffects(), "forged") {
				t.Fatal("resolved removal exposed mutable effects")
			}
		})
	}
}

func TestDelegatedRelationCarrierContractRejectsBoundaryMismatch(t *testing.T) {
	base := implementedCarrierContractCases()[0]
	carrier, subject, subjectKey, _ := mustCarrierFacts(t, base)
	entityID := mustContractEntityID(t, entity.KindExtension, base.declaration)
	wrongCarrier, _, _, _ := mustCarrierFacts(t, implementedCarrierContractCases()[2])

	tests := []struct {
		name     string
		entityID entity.ID
		carrier  desiredextension.CarrierKey
		subject  topology.SubjectID
		want     string
	}{
		{name: "wrong entity kind", entityID: mustContractEntityID(t, entity.KindSkill, base.declaration), subject: subject, carrier: carrier, want: "requires Extension entity"},
		{name: "wrong entity name", entityID: mustContractEntityID(t, entity.KindExtension, "other"), subject: subject, carrier: carrier, want: "does not match relation subject"},
		{name: "wrong carrier", entityID: entityID, subject: subject, carrier: wrongCarrier, want: "outside carrier"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewDelegatedRelationCarrierContract(test.entityID, test.carrier, test.subject, subjectKey)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
