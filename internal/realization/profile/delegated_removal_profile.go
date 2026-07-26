package profile

import (
	"fmt"
	"slices"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type delegatedRemovalDossierRow struct {
	scope       target.Scope
	sourceClass extensiontopology.CarrierSourceClass
	dossier     DelegatedRemovalDossier
}

// RemovalActuation identifies the boundary mechanism selected for one removal
// dossier. It describes no attempt outcome.
type RemovalActuation string

const (
	RemovalActuationHostRoute        RemovalActuation = "host_route"
	RemovalActuationDirectProjection RemovalActuation = "direct_projection"
)

// DelegatedRemovalDossier is the bounded current removal effect profile for
// one exact carrier source class and scope. It is neither host observation nor
// durable ownership authority.
type DelegatedRemovalDossier struct {
	actuation              RemovalActuation
	requiresExistingTrust  bool
	preservesSharedCarrier bool
	effectPostconditions   effectpostcondition.Set
	removedEffects         []string
	retainedEffects        []string
	nonClaims              []string
}

// Actuation returns the boundary mechanism selected for this removal.
func (dossier DelegatedRemovalDossier) Actuation() RemovalActuation {
	return dossier.actuation
}

// RequiresExistingTrust reports whether the selected scope must already be
// trusted before the route can run.
func (dossier DelegatedRemovalDossier) RequiresExistingTrust() bool {
	return dossier.requiresExistingTrust
}

// EffectPostconditions returns the exact route-coupled effect requirements.
func (dossier DelegatedRemovalDossier) EffectPostconditions() effectpostcondition.Set {
	return dossier.effectPostconditions
}

// PreservesSharedCarrier reports whether another daem-known consumer may
// safely share the exact structural carrier during this removal.
func (dossier DelegatedRemovalDossier) PreservesSharedCarrier() bool {
	return dossier.preservesSharedCarrier
}

// RemovedEffects returns the complete bounded deletion disclosure.
func (dossier DelegatedRemovalDossier) RemovedEffects() []string {
	return append([]string(nil), dossier.removedEffects...)
}

// RetainedEffects returns effects deliberately left in place.
func (dossier DelegatedRemovalDossier) RetainedEffects() []string {
	return append([]string(nil), dossier.retainedEffects...)
}

// NonClaims returns effects outside this removal route's authority.
func (dossier DelegatedRemovalDossier) NonClaims() []string {
	return append([]string(nil), dossier.nonClaims...)
}

// RemovalDossier resolves the static source-class and scope row for one exact
// desired carrier. A profile without a remove route or without a dossier for
// the exact source class returns admitted=false.
func (profile DelegatedRouteProfile) RemovalDossier(
	key desiredextension.CarrierKey,
) (DelegatedRemovalDossier, bool, error) {
	if err := profile.validate(); err != nil {
		return DelegatedRemovalDossier{}, false, err
	}
	if _, admitted := profile.OperationRoute(OperationRemove); !admitted {
		return DelegatedRemovalDossier{}, false, nil
	}
	if err := key.Validate(); err != nil {
		return DelegatedRemovalDossier{}, true, err
	}
	if key.Carrier() != profile.carrier || key.Target() != profile.target || !profile.supportsScope(key.Scope()) {
		return DelegatedRemovalDossier{}, true, fmt.Errorf(
			"removal carrier %q target %q scope %q does not match profile %q target %q",
			key.Carrier(),
			key.Target(),
			key.Scope(),
			profile.carrier,
			profile.target,
		)
	}
	source, err := extensiontopology.InterpretCarrierSource(key)
	if err != nil {
		return DelegatedRemovalDossier{}, true, err
	}
	var selected DelegatedRemovalDossier
	matches := 0
	for _, row := range profile.removalDossiers {
		if row.scope == key.Scope() && row.sourceClass == source.Class() {
			selected = cloneDelegatedRemovalDossier(row.dossier)
			matches++
		}
	}
	if matches == 0 {
		return DelegatedRemovalDossier{}, false, nil
	}
	if matches != 1 {
		return DelegatedRemovalDossier{}, true, fmt.Errorf(
			"removal profile %q has %d rows for scope %q source class %q",
			profile.carrier,
			matches,
			key.Scope(),
			source.Class(),
		)
	}
	return selected, true, nil
}

func validateDelegatedRemovalDossiers(profile DelegatedRouteProfile) error {
	_, hasRemoveRoute := profile.OperationRoute(OperationRemove)
	if hasRemoveRoute != (len(profile.removalDossiers) != 0) {
		return fmt.Errorf(
			"delegated carrier %q remove route and removal dossiers must be admitted together",
			profile.carrier,
		)
	}
	sourceClasses := extensiontopology.CarrierSourceClasses(profile.carrier)
	if hasRemoveRoute && len(sourceClasses) == 0 {
		return fmt.Errorf(
			"delegated carrier %q remove route requires an interpreted source-class vocabulary",
			profile.carrier,
		)
	}
	seen := make(map[string]struct{}, len(profile.removalDossiers))
	for _, row := range profile.removalDossiers {
		if !profile.supportsScope(row.scope) {
			return fmt.Errorf("delegated carrier %q removal dossier has unsupported scope %q", profile.carrier, row.scope)
		}
		if !slices.Contains(sourceClasses, row.sourceClass) {
			return fmt.Errorf("delegated carrier %q removal dossier has invalid source class %q", profile.carrier, row.sourceClass)
		}
		if err := validateDelegatedRemovalDossier(row.dossier); err != nil {
			return fmt.Errorf("delegated carrier %q removal dossier: %w", profile.carrier, err)
		}
		key := string(row.scope) + "\x00" + string(row.sourceClass)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf(
				"delegated carrier %q duplicates removal scope %q source class %q",
				profile.carrier,
				row.scope,
				row.sourceClass,
			)
		}
		seen[key] = struct{}{}
	}
	if hasRemoveRoute && len(seen) == 0 {
		return fmt.Errorf(
			"delegated carrier %q remove route requires at least one removal dossier",
			profile.carrier,
		)
	}
	if !hasRemoveRoute && profile.partialRemovalCoverage {
		return fmt.Errorf(
			"delegated carrier %q partial removal coverage requires a remove route",
			profile.carrier,
		)
	}
	if !profile.partialRemovalCoverage {
		for _, scope := range profile.allowedScopes {
			if !hasRemoveRoute {
				continue
			}
			for _, sourceClass := range sourceClasses {
				key := string(scope) + "\x00" + string(sourceClass)
				if _, exists := seen[key]; !exists {
					return fmt.Errorf(
						"delegated carrier %q removal dossiers omit scope %q source class %q",
						profile.carrier,
						scope,
						sourceClass,
					)
				}
			}
		}
	}
	return nil
}

func validateDelegatedRemovalDossier(dossier DelegatedRemovalDossier) error {
	switch dossier.actuation {
	case RemovalActuationHostRoute, RemovalActuationDirectProjection:
	default:
		return fmt.Errorf("removal actuation %q is unsupported", dossier.actuation)
	}
	if err := dossier.effectPostconditions.Validate(); err != nil {
		return err
	}
	if len(dossier.removedEffects) == 0 {
		return fmt.Errorf("removed effects are required")
	}
	owners := make(map[string]string)
	for _, field := range []struct {
		label  string
		values []string
	}{
		{label: "removed effect", values: dossier.removedEffects},
		{label: "retained effect", values: dossier.retainedEffects},
		{label: "removal non-claim", values: dossier.nonClaims},
	} {
		if err := validateStringSet(field.values, field.label); err != nil {
			return err
		}
		if !slices.Equal(field.values, canonicalStringSet(field.values)) {
			return fmt.Errorf("%s values must be canonical", field.label)
		}
		for _, value := range field.values {
			if owner, exists := owners[value]; exists {
				return fmt.Errorf("%s %q conflicts with %s", field.label, value, owner)
			}
			owners[value] = field.label
		}
	}
	return nil
}

func cloneDelegatedRemovalDossier(dossier DelegatedRemovalDossier) DelegatedRemovalDossier {
	dossier.removedEffects = append([]string(nil), dossier.removedEffects...)
	dossier.retainedEffects = append([]string(nil), dossier.retainedEffects...)
	dossier.nonClaims = append([]string(nil), dossier.nonClaims...)
	return dossier
}

type delegatedRemovalDossierInput struct {
	actuation              RemovalActuation
	requiresExistingTrust  bool
	preservesSharedCarrier bool
	effectPostconditions   []effectpostcondition.Requirement
	removedEffects         []string
	retainedEffects        []string
	nonClaims              []string
}

func newDelegatedRemovalDossier(input delegatedRemovalDossierInput) DelegatedRemovalDossier {
	return DelegatedRemovalDossier{
		actuation:              input.actuation,
		requiresExistingTrust:  input.requiresExistingTrust,
		preservesSharedCarrier: input.preservesSharedCarrier,
		effectPostconditions:   mustEffectPostconditionSet(input.effectPostconditions),
		removedEffects:         canonicalStringSet(input.removedEffects),
		retainedEffects:        canonicalStringSet(input.retainedEffects),
		nonClaims:              canonicalStringSet(input.nonClaims),
	}
}

func projectGlobalRemovalDossierRows(
	sourceClass extensiontopology.CarrierSourceClass,
	projectRequiresExistingTrust bool,
	input delegatedRemovalDossierInput,
) []delegatedRemovalDossierRow {
	projectInput := input
	projectInput.requiresExistingTrust = projectRequiresExistingTrust
	globalInput := input
	globalInput.requiresExistingTrust = false
	return []delegatedRemovalDossierRow{
		{
			scope:       target.ScopeProject,
			sourceClass: sourceClass,
			dossier:     newDelegatedRemovalDossier(projectInput),
		},
		{
			scope:       target.ScopeGlobal,
			sourceClass: sourceClass,
			dossier:     newDelegatedRemovalDossier(globalInput),
		},
	}
}

func claudePluginRemovalDossiers() []delegatedRemovalDossierRow {
	return projectGlobalRemovalDossierRows(
		extensiontopology.CarrierSourceMarketplace,
		false,
		delegatedRemovalDossierInput{
			actuation: RemovalActuationHostRoute,
			removedEffects: []string{
				"selected_claude_plugin_relation",
				"selected_effective_bundled_exposure",
			},
			retainedEffects: []string{
				"auto_installed_dependencies",
				"credentials_and_trust_session_state",
				"host_plugin_metadata",
				"marketplace_declaration",
				"orphaned_version_cache",
				"plugin_persistent_data",
				"sibling_plugin_relations_and_resources",
			},
			nonClaims: []string{
				"bundled_contribution_individual_cleanup",
				"credential_or_trust_cleanup",
				"exact_host_selected_artifact_replay",
				"external_store_prune",
				"runtime_unload_or_readiness",
			},
		},
	)
}

func codexPluginRemovalDossiers() []delegatedRemovalDossierRow {
	return []delegatedRemovalDossierRow{{
		scope:       target.ScopeGlobal,
		sourceClass: extensiontopology.CarrierSourceMarketplace,
		dossier: newDelegatedRemovalDossier(delegatedRemovalDossierInput{
			actuation: RemovalActuationHostRoute,
			effectPostconditions: []effectpostcondition.Requirement{
				effectpostcondition.CarrierArtifactsAbsent,
			},
			removedEffects: []string{
				"selected_codex_plugin_relation",
				"selected_effective_bundled_exposure",
				"selected_versioned_bundle_cache",
			},
			retainedEffects: []string{
				"credentials_and_trust_session_state",
				"marketplace_declaration",
				"marketplace_snapshot",
				"sibling_plugin_relations_and_caches",
				"unrelated_codex_config",
			},
			nonClaims: []string{
				"bundled_contribution_individual_cleanup",
				"credential_or_trust_cleanup",
				"exact_host_selected_artifact_replay",
				"external_store_prune",
				"marketplace_removal_or_prune",
				"runtime_unload_or_readiness",
			},
		}),
	}}
}

func piPackageRemovalDossiers() []delegatedRemovalDossierRow {
	commonRemoved := []string{"selected_package_resources", "selected_pi_package_relation"}
	commonRetained := []string{
		"pi_executable",
		"sibling_package_resources",
		"trust_session_and_model_state",
		"unrelated_package_relations",
	}
	commonNonClaims := []string{
		"credential_or_trust_cleanup",
		"exact_host_selected_artifact_replay",
		"external_store_prune",
		"runtime_unload_or_readiness",
	}
	rows := projectGlobalRemovalDossierRows(
		extensiontopology.CarrierSourceNPM,
		true,
		delegatedRemovalDossierInput{
			actuation:            RemovalActuationHostRoute,
			effectPostconditions: []effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
			removedEffects: append(
				append([]string(nil), commonRemoved...),
				"native_unused_transitive_dependencies",
				"selected_scoped_npm_package_artifacts",
			),
			retainedEffects: append(
				append([]string(nil), commonRetained...),
				"npm_cache_and_logs",
				"npm_install_root_metadata",
			),
			nonClaims: append(
				append([]string(nil), commonNonClaims...),
				"dependency_gc_beyond_native_remove",
			),
		},
	)
	rows = append(rows, projectGlobalRemovalDossierRows(
		extensiontopology.CarrierSourceGit,
		true,
		delegatedRemovalDossierInput{
			actuation:            RemovalActuationHostRoute,
			effectPostconditions: []effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
			removedEffects: append(
				append([]string(nil), commonRemoved...),
				"selected_scoped_git_checkout",
			),
			retainedEffects: append(
				append([]string(nil), commonRetained...),
				"git_install_root",
				"unrelated_git_checkouts",
			),
			nonClaims: append([]string(nil), commonNonClaims...),
		},
	)...)
	rows = append(rows, projectGlobalRemovalDossierRows(
		extensiontopology.CarrierSourceLocal,
		true,
		delegatedRemovalDossierInput{
			actuation:            RemovalActuationHostRoute,
			effectPostconditions: []effectpostcondition.Requirement{effectpostcondition.LocalSourceUnchanged},
			removedEffects:       append([]string(nil), commonRemoved...),
			retainedEffects: append(
				append([]string(nil), commonRetained...),
				"local_source_directory",
			),
			nonClaims: append([]string(nil), commonNonClaims...),
		},
	)...)
	return rows
}

func openCodePluginRemovalDossiers() []delegatedRemovalDossierRow {
	return projectGlobalRemovalDossierRows(
		extensiontopology.CarrierSourceHost,
		false,
		delegatedRemovalDossierInput{
			actuation: RemovalActuationDirectProjection,
			removedEffects: []string{
				"selected_opencode_plugin_config_relation",
			},
			retainedEffects: []string{
				"credentials_and_trust_session_state",
				"dependency_lockfiles",
				"local_source_directories",
				"npm_cache_and_logs",
				"package_manager_installations",
				"plugin_persistent_data",
				"runtime_residue",
				"sibling_plugin_relations_and_resources",
			},
			nonClaims: []string{
				"credential_or_trust_cleanup",
				"external_store_prune",
				"merged_runtime_layer_absence",
				"package_or_dependency_uninstall",
				"runtime_unload_or_readiness",
				"unsupported_config_layer_cleanup",
			},
		},
	)
}

func antigravityCLIPluginRemovalDossiers() []delegatedRemovalDossierRow {
	return []delegatedRemovalDossierRow{{
		scope:       target.ScopeGlobal,
		sourceClass: extensiontopology.CarrierSourceMarketplace,
		dossier: newDelegatedRemovalDossier(delegatedRemovalDossierInput{
			actuation: RemovalActuationHostRoute,
			effectPostconditions: []effectpostcondition.Requirement{
				effectpostcondition.CarrierArtifactsAbsent,
			},
			removedEffects: []string{
				"selected_antigravity_cli_plugin_bundle",
				"selected_antigravity_cli_plugin_import_relation",
				"selected_effective_bundled_exposure",
			},
			retainedEffects: []string{
				"antigravity_ide_state",
				"credentials_and_trust_session_state",
				"external_source_and_marketplace_state",
				"sibling_plugin_relations_and_bundles",
				"unrelated_antigravity_cli_config",
			},
			nonClaims: []string{
				"antigravity_ide_support",
				"bundled_contribution_individual_cleanup",
				"credential_or_trust_cleanup",
				"exact_host_selected_artifact_replay",
				"external_store_prune",
				"runtime_unload_or_readiness",
				"source_provenance_reverification",
			},
		}),
	}}
}

func mustEffectPostconditionSet(
	requirements []effectpostcondition.Requirement,
) effectpostcondition.Set {
	set, err := effectpostcondition.NewSet(requirements)
	if err != nil {
		panic(err)
	}
	return set
}
