package lock

import (
	"fmt"
	"strings"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

var claudePluginCarrierSpec = delegatedRelationCarrierSpec{
	Label:     "Claude plugin carrier",
	Profile:   mustProfileDelegatedRoute(target.TargetClaudeCode, desiredextension.CarrierClaudeCodePlugin),
	Ownership: OwnershipManifest,
	OnAbsent:  OnAbsentBlock,
	Replay: delegatedRelationReplaySpec{
		Invocation: ReplayPartial,
		Outcome:    ReplayUnavailable,
		Derivation: ReplayNotApplicable,
		Exclusions: []ReplayExclusion{
			{Component: "host-selected plugin artifact or version", Reason: ReplayExclusionHostSelectedArtifact},
			{Component: "Claude marketplace/source selected outcome", Reason: ReplayExclusionHostMarketplace},
			{Component: "package manager, plugin cache, and dependency state", Reason: ReplayExclusionRuntimeDependency},
			{Component: "Claude trust or approval state", Reason: ReplayExclusionHostApproval},
			{Component: "plugin activation, reload, and runtime readiness", Reason: ReplayExclusionRuntimeReadiness},
			{Component: "plugin-bundled tool or MCP inventory", Reason: ReplayExclusionToolInventory},
		},
	},
	OperationContracts: []OperationContractInput{
		{
			Operation:       OperationObserve,
			Actuation:       ActuationNoMutation,
			Authority:       AuthorityObserve,
			EffectEnvelope:  EffectEnvelopeNotApplicable,
			Idempotency:     IdempotencyNotApplicable,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationNotRequired,
			Recovery:        OperationRecoveryNotApplicable,
		},
		{
			Operation: OperationInstall,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityManage,
			Preconditions: []string{
				"passive_inventory_fresh",
				"managed_instance_correlation_known",
				"same_name_unmanaged_absent",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
		{
			Operation: OperationRefresh,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityNone,
			Preconditions: []string{
				"exact_relation_present",
				"explicit_refresh_intent",
				"passive_inventory_fresh",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
	},
}

var openCodePluginCarrierSpec = delegatedRelationCarrierSpec{
	Label:     "OpenCode plugin carrier",
	Profile:   mustProfileDelegatedRoute(target.TargetOpenCode, desiredextension.CarrierOpenCodePlugin),
	Ownership: OwnershipManifest,
	OnAbsent:  OnAbsentBlock,
	Replay: delegatedRelationReplaySpec{
		Invocation: ReplayPartial,
		Outcome:    ReplayUnavailable,
		Derivation: ReplayNotApplicable,
		Exclusions: []ReplayExclusion{
			{Component: "host-selected plugin artifact or version", Reason: ReplayExclusionHostSelectedArtifact},
			{Component: "OpenCode host source selected outcome", Reason: ReplayExclusionHostSource},
			{Component: "OpenCode plugin cache and dependency state", Reason: ReplayExclusionRuntimeDependency},
			{Component: "OpenCode trust, auth, or session state", Reason: ReplayExclusionHostApproval},
			{Component: "plugin activation and runtime readiness", Reason: ReplayExclusionRuntimeReadiness},
			{Component: "plugin-bundled MCP, command, hook, or tool inventory", Reason: ReplayExclusionToolInventory},
		},
	},
	OperationContracts: []OperationContractInput{
		{
			Operation:       OperationObserve,
			Actuation:       ActuationNoMutation,
			Authority:       AuthorityObserve,
			EffectEnvelope:  EffectEnvelopeNotApplicable,
			Idempotency:     IdempotencyNotApplicable,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationNotRequired,
			Recovery:        OperationRecoveryNotApplicable,
		},
		{
			Operation: OperationInstall,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityManage,
			Preconditions: []string{
				"passive_inventory_fresh",
				"managed_instance_correlation_known",
				"same_source_unmanaged_absent",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
		{
			Operation: OperationRefresh,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityNone,
			Preconditions: []string{
				"exact_relation_present",
				"explicit_refresh_intent",
				"passive_inventory_fresh",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationInsufficient,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
	},
}

var piPackageCarrierSpec = delegatedRelationCarrierSpec{
	Label:     "Pi package carrier",
	Profile:   mustProfileDelegatedRoute(target.TargetPi, desiredextension.CarrierPiPackage),
	Ownership: OwnershipManifest,
	OnAbsent:  OnAbsentBlock,
	Replay: delegatedRelationReplaySpec{
		Invocation: ReplayPartial,
		Outcome:    ReplayUnavailable,
		Derivation: ReplayNotApplicable,
		Exclusions: []ReplayExclusion{
			{Component: "host-selected package artifact or version", Reason: ReplayExclusionHostSelectedArtifact},
			{Component: "Pi host source selected outcome", Reason: ReplayExclusionHostSource},
			{Component: "Pi package cache and dependency state", Reason: ReplayExclusionRuntimeDependency},
			{Component: "Pi trust, auth, or session state", Reason: ReplayExclusionHostApproval},
			{Component: "package activation and runtime readiness", Reason: ReplayExclusionRuntimeReadiness},
			{Component: "package-bundled MCP, command, hook, or tool inventory", Reason: ReplayExclusionToolInventory},
		},
	},
	OperationContracts: []OperationContractInput{
		{
			Operation:       OperationObserve,
			Actuation:       ActuationNoMutation,
			Authority:       AuthorityObserve,
			EffectEnvelope:  EffectEnvelopeNotApplicable,
			Idempotency:     IdempotencyNotApplicable,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationNotRequired,
			Recovery:        OperationRecoveryNotApplicable,
		},
		{
			Operation: OperationInstall,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityManage,
			Preconditions: []string{
				"passive_inventory_fresh",
				"managed_instance_correlation_known",
				"same_name_unmanaged_absent",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationHostRelation,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
		{
			Operation: OperationRefresh,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityNone,
			Preconditions: []string{
				"explicit_refresh_intent",
				"passive_inventory_unsupported",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationInsufficient,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
	},
}

var antigravityCLIPluginCarrierSpec = delegatedRelationCarrierSpec{
	Label:     "Antigravity CLI plugin carrier",
	Profile:   mustProfileDelegatedRoute(target.TargetAntigravityCLI, desiredextension.CarrierAntigravityCLIPlugin),
	Ownership: OwnershipManifest,
	OnAbsent:  OnAbsentBlock,
	Replay: delegatedRelationReplaySpec{
		Invocation: ReplayPartial,
		Outcome:    ReplayUnavailable,
		Derivation: ReplayNotApplicable,
		Exclusions: []ReplayExclusion{
			{Component: "host-selected plugin artifact or version", Reason: ReplayExclusionHostSelectedArtifact},
			{Component: "Antigravity CLI host source selected outcome", Reason: ReplayExclusionHostSource},
			{Component: "Antigravity CLI plugin staging, cache, and dependency state", Reason: ReplayExclusionRuntimeDependency},
			{Component: "Antigravity CLI trust, auth, or session state", Reason: ReplayExclusionHostApproval},
			{Component: "plugin activation and runtime readiness", Reason: ReplayExclusionRuntimeReadiness},
			{Component: "plugin-bundled MCP, hook, skill, agent, rule, or tool inventory", Reason: ReplayExclusionToolInventory},
		},
	},
	OperationContracts: []OperationContractInput{
		{
			Operation: OperationInstall,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityManage,
			Preconditions: []string{
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationInsufficient,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
		{
			Operation: OperationRefresh,
			Actuation: ActuationDelegatedHostRoute,
			Authority: AuthorityNone,
			Preconditions: []string{
				"explicit_refresh_intent",
				"passive_inventory_unsupported",
				"route_dossier_admitted",
			},
			EffectEnvelope:  EffectEnvelopeIncomplete,
			Idempotency:     IdempotencyUnknown,
			Verification:    VerificationInsufficient,
			TrustActivation: TrustActivationUnknown,
			Recovery:        OperationRecoveryUnknown,
		},
	},
}

func implementedDelegatedRelationCarrierSpecs() []delegatedRelationCarrierSpec {
	return []delegatedRelationCarrierSpec{
		claudePluginCarrierSpec,
		codexPluginCarrierSpec,
		openCodePluginCarrierSpec,
		piPackageCarrierSpec,
		antigravityCLIPluginCarrierSpec,
	}
}

func validateDelegatedRelationCarrierSpec(spec delegatedRelationCarrierSpec) error {
	if strings.TrimSpace(spec.Label) == "" {
		return fmt.Errorf("delegated relation carrier label is required")
	}
	if err := spec.Profile.Validate(); err != nil {
		return fmt.Errorf("%s profile: %w", spec.Label, err)
	}
	if _, err := desiredextension.ParseCarrier(string(spec.Profile.Carrier())); err != nil {
		return fmt.Errorf("%s carrier: %w", spec.Label, err)
	}
	if len(spec.OperationContracts) == 0 {
		return fmt.Errorf("%s operation contracts are required", spec.Label)
	}
	return nil
}

func validateDelegatedRelationCarrierIdentity(
	carrier desiredextension.CarrierKey,
	subject topology.SubjectID,
	spec delegatedRelationCarrierSpec,
) error {
	if err := carrier.Validate(); err != nil {
		return err
	}
	if carrier.Carrier() != spec.Profile.Carrier() {
		return fmt.Errorf(
			"%s carrier = %q, want %q",
			spec.Label,
			carrier.Carrier(),
			spec.Profile.Carrier(),
		)
	}
	if carrier.Target() != spec.Profile.Target() {
		return fmt.Errorf(
			"%s target = %q, want %q",
			spec.Label,
			carrier.Target(),
			spec.Profile.Target(),
		)
	}
	if !extensiontopology.IsCarrierRelation(spec.Profile.Carrier(), subject) {
		return fmt.Errorf(
			"%s topology subject %q is outside carrier %q",
			spec.Label,
			subject,
			spec.Profile.Carrier(),
		)
	}
	return nil
}
