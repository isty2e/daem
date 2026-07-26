package profile

import (
	"fmt"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
)

const (
	claudeCodePluginPlacementID     = "claude-code-plugin"
	claudeCodePluginInstallRouteID  = "claude-code.plugin-carrier.install"
	claudeCodePluginAdapterV1       = "claude-plugin-carrier-v1"
	claudeCodePluginRefreshRouteID  = "claude-code.plugin-carrier.refresh"
	claudeCodePluginRefreshV1       = "claude-plugin-refresh-v1"
	claudeCodePluginRemoveRouteID   = "claude-code.plugin-carrier.remove"
	claudeCodePluginRemoveV1        = "claude-plugin-remove-v1"
	codexPluginPlacementID          = "codex-plugin"
	codexPluginInstallRouteID       = "codex.plugin-carrier.install"
	codexPluginAdapterV1            = "codex-plugin-carrier-v1"
	codexPluginRefreshRouteID       = "codex.plugin-marketplace.refresh"
	codexPluginRefreshV1            = "codex-plugin-marketplace-refresh-v1"
	codexPluginRemoveRouteID        = "codex.plugin-carrier.remove"
	codexPluginRemoveV1             = "codex-plugin-remove-v1"
	openCodePluginPlacementID       = "opencode-plugin"
	openCodePluginInstallRouteID    = "opencode.plugin-carrier.install"
	openCodePluginAdapterV1         = "opencode-plugin-carrier-v1"
	openCodePluginRefreshRouteID    = "opencode.plugin-carrier.refresh"
	openCodePluginRefreshV1         = "opencode-plugin-refresh-v1"
	openCodePluginRemoveRouteID     = "opencode.plugin-config-relation.remove"
	openCodePluginRemoveV1          = "opencode-plugin-config-relation-v1"
	piPackagePlacementID            = "pi-package"
	piPackageInstallRouteID         = "pi.package-carrier.install"
	piPackageAdapterV1              = "pi-package-carrier-v1"
	piPackageRefreshRouteID         = "pi.package-carrier.refresh"
	piPackageRefreshV1              = "pi-package-refresh-v1"
	piPackageRemoveRouteID          = "pi.package-carrier.remove"
	piPackageRemoveV1               = "pi-package-remove-v1"
	antigravityCLIPluginPlacementID = "antigravity-cli-plugin"
	antigravityCLIPluginInstallID   = "antigravity-cli.plugin-carrier.install"
	antigravityCLIPluginAdapterV1   = "antigravity-cli-plugin-carrier-v1"
	antigravityCLIPluginRefreshID   = "antigravity-cli.plugin-carrier.refresh"
	antigravityCLIPluginRefreshV1   = "antigravity-cli-plugin-refresh-v1"
	antigravityCLIPluginRemoveID    = "antigravity-cli.plugin-carrier.remove"
	antigravityCLIPluginRemoveV1    = "antigravity-cli-plugin-remove-v1"
)

var delegatedRelationVerifiedFields = []string{
	"managed_instance_key",
	"relation_subject_key",
	"scope",
	"source_kind",
	"source_ref",
	"target",
}

// DelegatedRouteProfile selects a carrier, target, admitted scopes, and a
// closed operation-indexed route set. It owns no host attempt outcome.
type DelegatedRouteProfile struct {
	carrier                desiredextension.Carrier
	target                 target.Target
	allowedScopes          []target.Scope
	operationRoutes        []OperationRoute
	removalDossiers        []delegatedRemovalDossierRow
	partialRemovalCoverage bool
	verifiedRelationFields []string
}

// DelegatedRelationProfileInput carries desired relation facts not owned by the static profile.
type DelegatedRelationProfileInput struct {
	Scope                target.Scope
	SourceNamespace      string
	ExpectedRelation     hostrelation.ExpectedRelation
	CanonicalRequestHash string
}

func (profile DelegatedRouteProfile) Carrier() desiredextension.Carrier { return profile.carrier }
func (profile DelegatedRouteProfile) Target() target.Target             { return profile.target }

// OperationRoute returns the unique route for one operation.
func (profile DelegatedRouteProfile) OperationRoute(operation Operation) (OperationRoute, bool) {
	var selected OperationRoute
	count := 0
	for _, route := range profile.operationRoutes {
		if route.Operation() == operation {
			selected = route
			count++
		}
	}
	return selected, count == 1
}

// OperationRoutes returns the stable operation-indexed route set.
func (profile DelegatedRouteProfile) OperationRoutes() []OperationRoute {
	return append([]OperationRoute(nil), profile.operationRoutes...)
}

// Validate rejects a zero or malformed static delegated route profile.
func (profile DelegatedRouteProfile) Validate() error { return profile.validate() }

// Realize constructs one delegated relation using this profile's static route facts.
func (profile DelegatedRouteProfile) Realize(input DelegatedRelationProfileInput) (realization.RealizationSpec, error) {
	if err := profile.validate(); err != nil {
		return realization.RealizationSpec{}, err
	}
	if !profile.supportsScope(input.Scope) {
		return realization.RealizationSpec{}, fmt.Errorf(
			"delegated route for carrier %q does not support scope %q",
			profile.carrier,
			input.Scope,
		)
	}
	installRoute, ok := profile.OperationRoute(OperationInstall)
	if !ok {
		return realization.RealizationSpec{}, fmt.Errorf(
			"delegated route for carrier %q has no unique install route",
			profile.carrier,
		)
	}
	return realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID:            installRoute.CorrelationID(),
		Target:                 profile.target,
		Scope:                  input.Scope,
		SourceNamespace:        input.SourceNamespace,
		ExpectedRelation:       input.ExpectedRelation,
		RouteID:                installRoute.RouteID(),
		RouteContractVersion:   installRoute.AdapterContractVersion(),
		CanonicalRequestHash:   input.CanonicalRequestHash,
		VerifiedRelationFields: profile.verifiedRelationFields,
	})
}

func (profile DelegatedRouteProfile) supportsScope(scope target.Scope) bool {
	return slices.Contains(profile.allowedScopes, scope)
}

func (profile DelegatedRouteProfile) validate() error {
	if _, err := desiredextension.ParseCarrier(string(profile.carrier)); err != nil {
		return err
	}
	if _, err := target.ParseTarget(string(profile.target)); err != nil {
		return err
	}
	if len(profile.allowedScopes) == 0 {
		return fmt.Errorf("delegated route %q requires at least one scope", profile.carrier)
	}
	for index, scope := range profile.allowedScopes {
		if _, err := target.ParseScope(string(scope)); err != nil {
			return err
		}
		if !profile.carrier.AdmitsTargetScope(profile.target, scope) {
			return fmt.Errorf("delegated route carrier %q does not admit target %q scope %q", profile.carrier, profile.target, scope)
		}
		if index > 0 && profile.allowedScopes[index-1] >= scope {
			return fmt.Errorf("delegated route %q scopes must be sorted and unique", profile.carrier)
		}
	}
	if admittedScopes := profile.carrier.AdmittedScopes(); !slices.Equal(profile.allowedScopes, admittedScopes) {
		return fmt.Errorf(
			"delegated route %q scopes %v must exactly match desired carrier scopes %v",
			profile.carrier,
			profile.allowedScopes,
			admittedScopes,
		)
	}
	if len(profile.operationRoutes) == 0 {
		return fmt.Errorf("delegated carrier %q requires operation routes", profile.carrier)
	}
	seenOperations := make(map[Operation]struct{}, len(profile.operationRoutes))
	correlationID := ""
	for _, route := range profile.operationRoutes {
		if err := route.Validate(); err != nil {
			return err
		}
		if route.ResourceKind() != entity.KindExtension {
			return fmt.Errorf(
				"delegated carrier %q operation %q requires extension resource kind",
				profile.carrier,
				route.Operation(),
			)
		}
		if _, duplicate := seenOperations[route.Operation()]; duplicate {
			return fmt.Errorf(
				"delegated carrier %q has duplicate %s route",
				profile.carrier,
				route.Operation(),
			)
		}
		seenOperations[route.Operation()] = struct{}{}
		if correlationID == "" {
			correlationID = route.CorrelationID()
		} else if route.CorrelationID() != correlationID {
			return fmt.Errorf(
				"delegated carrier %q operation routes have conflicting correlations %q and %q",
				profile.carrier,
				correlationID,
				route.CorrelationID(),
			)
		}
	}
	if err := validateDelegatedRemovalDossiers(profile); err != nil {
		return err
	}
	return validateStringSet(profile.verifiedRelationFields, "delegated route verified field")
}

func profileDelegatedRoutes(selectedTarget target.Target) []DelegatedRouteProfile {
	result := make([]DelegatedRouteProfile, 0)
	for _, profile := range delegatedRouteProfiles {
		if profile.target == selectedTarget {
			result = append(result, cloneDelegatedRouteProfile(profile))
		}
	}
	return result
}

func cloneDelegatedRouteProfile(profile DelegatedRouteProfile) DelegatedRouteProfile {
	profile.allowedScopes = append([]target.Scope(nil), profile.allowedScopes...)
	profile.operationRoutes = append([]OperationRoute(nil), profile.operationRoutes...)
	profile.removalDossiers = append([]delegatedRemovalDossierRow(nil), profile.removalDossiers...)
	for index := range profile.removalDossiers {
		profile.removalDossiers[index].dossier = cloneDelegatedRemovalDossier(
			profile.removalDossiers[index].dossier,
		)
	}
	profile.verifiedRelationFields = append([]string(nil), profile.verifiedRelationFields...)
	return profile
}

func mustDelegatedRouteProfile(profile DelegatedRouteProfile) DelegatedRouteProfile {
	slices.Sort(profile.allowedScopes)
	sort.Slice(profile.operationRoutes, func(left int, right int) bool {
		return profile.operationRoutes[left].Operation() < profile.operationRoutes[right].Operation()
	})
	profile.verifiedRelationFields = canonicalStringSet(profile.verifiedRelationFields)
	if err := profile.validate(); err != nil {
		panic(err)
	}
	return profile
}

var delegatedRouteProfiles = []DelegatedRouteProfile{
	newDelegatedRouteProfileWithRemoval(desiredextension.CarrierClaudeCodePlugin, target.TargetClaudeCode,
		claudeCodePluginPlacementID,
		claudePluginRemovalDossiers(),
		delegatedOperationRoute(OperationInstall, claudeCodePluginInstallRouteID, claudeCodePluginAdapterV1),
		delegatedOperationRoute(OperationRemove, claudeCodePluginRemoveRouteID, claudeCodePluginRemoveV1),
		delegatedOperationRoute(OperationRefresh, claudeCodePluginRefreshRouteID, claudeCodePluginRefreshV1)),
	newDelegatedRouteProfileWithRemoval(desiredextension.CarrierCodexPlugin, target.TargetCodex,
		codexPluginPlacementID,
		codexPluginRemovalDossiers(),
		delegatedOperationRoute(OperationInstall, codexPluginInstallRouteID, codexPluginAdapterV1),
		delegatedOperationRoute(OperationRemove, codexPluginRemoveRouteID, codexPluginRemoveV1),
		delegatedOperationRoute(OperationRefresh, codexPluginRefreshRouteID, codexPluginRefreshV1)),
	newDelegatedRouteProfileWithRemoval(desiredextension.CarrierOpenCodePlugin, target.TargetOpenCode,
		openCodePluginPlacementID,
		openCodePluginRemovalDossiers(),
		delegatedOperationRoute(OperationInstall, openCodePluginInstallRouteID, openCodePluginAdapterV1),
		delegatedOperationRoute(OperationRemove, openCodePluginRemoveRouteID, openCodePluginRemoveV1),
		delegatedOperationRoute(OperationRefresh, openCodePluginRefreshRouteID, openCodePluginRefreshV1)),
	newDelegatedRouteProfileWithRemoval(desiredextension.CarrierPiPackage, target.TargetPi,
		piPackagePlacementID,
		piPackageRemovalDossiers(),
		delegatedOperationRoute(OperationInstall, piPackageInstallRouteID, piPackageAdapterV1),
		delegatedOperationRoute(OperationRemove, piPackageRemoveRouteID, piPackageRemoveV1),
		delegatedOperationRoute(OperationRefresh, piPackageRefreshRouteID, piPackageRefreshV1)),
	newDelegatedRouteProfileWithRemovalSubset(desiredextension.CarrierAntigravityCLIPlugin, target.TargetAntigravityCLI,
		antigravityCLIPluginPlacementID,
		antigravityCLIPluginRemovalDossiers(),
		delegatedOperationRoute(OperationInstall, antigravityCLIPluginInstallID, antigravityCLIPluginAdapterV1),
		delegatedOperationRoute(OperationRemove, antigravityCLIPluginRemoveID, antigravityCLIPluginRemoveV1),
		delegatedOperationRoute(OperationRefresh, antigravityCLIPluginRefreshID, antigravityCLIPluginRefreshV1)),
}

type delegatedOperationRouteSpec struct {
	operation              Operation
	routeID                string
	adapterContractVersion string
}

func delegatedOperationRoute(
	operation Operation,
	routeID string,
	adapterContractVersion string,
) delegatedOperationRouteSpec {
	return delegatedOperationRouteSpec{
		operation:              operation,
		routeID:                routeID,
		adapterContractVersion: adapterContractVersion,
	}
}

func newDelegatedRouteProfile(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	correlationID string,
	routeSpecs ...delegatedOperationRouteSpec,
) DelegatedRouteProfile {
	return mustDelegatedRouteProfile(newDelegatedRouteProfileValue(
		carrier,
		selectedTarget,
		correlationID,
		routeSpecs...,
	))
}

func newDelegatedRouteProfileWithRemoval(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	correlationID string,
	removalDossiers []delegatedRemovalDossierRow,
	routeSpecs ...delegatedOperationRouteSpec,
) DelegatedRouteProfile {
	profile := newDelegatedRouteProfileValue(carrier, selectedTarget, correlationID, routeSpecs...)
	profile.removalDossiers = append([]delegatedRemovalDossierRow(nil), removalDossiers...)
	return mustDelegatedRouteProfile(profile)
}

func newDelegatedRouteProfileWithRemovalSubset(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	correlationID string,
	removalDossiers []delegatedRemovalDossierRow,
	routeSpecs ...delegatedOperationRouteSpec,
) DelegatedRouteProfile {
	profile := newDelegatedRouteProfileValue(
		carrier,
		selectedTarget,
		correlationID,
		routeSpecs...,
	)
	profile.removalDossiers = append(
		[]delegatedRemovalDossierRow(nil),
		removalDossiers...,
	)
	profile.partialRemovalCoverage = true
	return mustDelegatedRouteProfile(profile)
}

func newDelegatedRouteProfileValue(
	carrier desiredextension.Carrier,
	selectedTarget target.Target,
	correlationID string,
	routeSpecs ...delegatedOperationRouteSpec,
) DelegatedRouteProfile {
	operationRoutes := make([]OperationRoute, 0, len(routeSpecs))
	for _, route := range routeSpecs {
		operationRoutes = append(operationRoutes, mustOperationRoute(
			entity.KindExtension,
			route.operation,
			correlationID,
			route.routeID,
			route.adapterContractVersion,
		))
	}
	return DelegatedRouteProfile{
		carrier:                carrier,
		target:                 selectedTarget,
		allowedScopes:          carrier.AdmittedScopes(),
		operationRoutes:        operationRoutes,
		verifiedRelationFields: delegatedRelationVerifiedFields,
	}
}

func init() {
	if err := validateDelegatedRouteProfileCatalog(delegatedRouteProfiles); err != nil {
		panic(err)
	}
}

func validateDelegatedRouteProfileCatalog(profiles []DelegatedRouteProfile) error {
	seenCarriers := make(map[desiredextension.Carrier]struct{}, len(profiles))
	seenRoutes := make(map[string]struct{}, len(profiles))
	seenCorrelations := make(map[string]desiredextension.Carrier, len(profiles))
	for _, profile := range profiles {
		if err := profile.validate(); err != nil {
			return err
		}
		if _, exists := seenCarriers[profile.carrier]; exists {
			return fmt.Errorf("duplicate delegated route carrier %q", profile.carrier)
		}
		seenCarriers[profile.carrier] = struct{}{}
		for _, route := range profile.operationRoutes {
			if _, exists := seenRoutes[route.RouteID()]; exists {
				return fmt.Errorf("duplicate delegated route id %q", route.RouteID())
			}
			seenRoutes[route.RouteID()] = struct{}{}
			if owner, exists := seenCorrelations[route.CorrelationID()]; exists && owner != profile.carrier {
				return fmt.Errorf(
					"delegated route correlation id %q is shared by carriers %q and %q",
					route.CorrelationID(),
					owner,
					profile.carrier,
				)
			}
			seenCorrelations[route.CorrelationID()] = profile.carrier
		}
	}
	supportedCarriers := desiredextension.SupportedCarriers()
	if len(seenCarriers) != len(supportedCarriers) {
		return fmt.Errorf(
			"delegated route profile catalog covers %d carriers, want all %d supported carriers",
			len(seenCarriers),
			len(supportedCarriers),
		)
	}
	for _, carrier := range supportedCarriers {
		if _, exists := seenCarriers[carrier]; !exists {
			return fmt.Errorf("supported extension carrier %q is missing a delegated route profile", carrier)
		}
	}
	return nil
}
