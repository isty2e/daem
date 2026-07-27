package authoring

import (
	"fmt"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

func extensionAuthoringCarrierFor(value string) (desiredextension.Carrier, bool) {
	carrier, err := desiredextension.ParseCarrier(value)
	return carrier, err == nil
}

func extensionAuthoringCarrierForTarget(selectedTarget string) (desiredextension.Carrier, bool) {
	parsedTarget, err := target.ParseTarget(selectedTarget)
	if err != nil {
		return "", false
	}
	var matched desiredextension.Carrier
	for _, carrier := range desiredextension.SupportedCarriers() {
		admittedTarget, ok := carrier.AdmittedTarget()
		if ok && admittedTarget == parsedTarget {
			if matched != "" {
				return "", false
			}
			matched = carrier
		}
	}
	return matched, matched != ""
}

func extensionAuthoringCarrierFromAddRequest(request AddExtensionRequest, header declaration.ManifestHeader, origin daempaths.ManifestOrigin) (desiredextension.Carrier, error) {
	if len(request.Targets) != 1 {
		if len(request.Targets) > 1 {
			return "", fmt.Errorf("extension authoring accepts at most one distinct --target")
		}
		return inferExtensionAuthoringCarrier(request, header, origin)
	}
	carrier, ok := extensionAuthoringCarrierForTarget(request.Targets[0])
	if !ok {
		return "", fmt.Errorf(
			"extension authoring does not support --target %s; supported targets are %s",
			request.Targets[0],
			extensionAuthoringTargetSummary(),
		)
	}
	return carrier, nil
}

func inferExtensionAuthoringCarrier(request AddExtensionRequest, header declaration.ManifestHeader, origin daempaths.ManifestOrigin) (desiredextension.Carrier, error) {
	eligible := make([]desiredextension.Carrier, 0, len(header.Targets))
	seen := make(map[target.Target]struct{}, len(header.Targets))
	for _, targetValue := range header.Targets {
		carrier, ok := extensionAuthoringCarrierForTarget(targetValue)
		if !ok {
			continue
		}
		admittedTarget, _ := carrier.AdmittedTarget()
		if _, exists := seen[admittedTarget]; exists {
			continue
		}
		rawScope := extensionAuthoringScopeInput(request.Scope, origin)
		if _, err := extensionAuthoringScope(carrier, rawScope); err != nil {
			continue
		}
		seen[admittedTarget] = struct{}{}
		eligible = append(eligible, carrier)
	}
	if len(eligible) == 1 {
		return eligible[0], nil
	}
	candidates := make([]desiredextension.Carrier, 0, len(eligible))
	for _, carrier := range eligible {
		if _, err := extensionAuthoringSource(carrier, request.Source); err == nil {
			candidates = append(candidates, carrier)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], nil
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("extension source %q does not identify an admitted row from manifest targets; pass --target and, for global authority, --scope global", request.Source)
	}
	names := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		admittedTarget, _ := candidate.AdmittedTarget()
		names = append(names, string(admittedTarget))
	}
	sort.Strings(names)
	return "", fmt.Errorf("extension source %q is ambiguous across manifest targets %s; pass one --target", request.Source, strings.Join(names, ", "))
}

func extensionAuthoringScopeInput(requestScope string, origin daempaths.ManifestOrigin) string {
	if strings.TrimSpace(requestScope) != "" {
		return requestScope
	}
	if origin == daempaths.ManifestOriginUserDefault {
		return string(target.ScopeGlobal)
	}
	return ""
}

func extensionAuthoringTargetSummary() string {
	targets := make([]string, 0, len(desiredextension.SupportedCarriers()))
	for _, carrier := range desiredextension.SupportedCarriers() {
		admittedTarget, ok := carrier.AdmittedTarget()
		if ok {
			targets = append(targets, string(admittedTarget))
		}
	}
	return strings.Join(targets, ", ")
}

func (request RemoveExtensionRequest) normalizedSelector() (RemoveExtensionRequest, error) {
	if len(request.Targets) > 1 {
		return RemoveExtensionRequest{}, fmt.Errorf("extension removal accepts at most one --target filter")
	}

	scope := strings.TrimSpace(request.Scope)
	if scope != "" && scope != string(target.ScopeProject) && scope != string(target.ScopeGlobal) {
		return RemoveExtensionRequest{}, fmt.Errorf("extension removal supports only --scope %s or %s", target.ScopeProject, target.ScopeGlobal)
	}
	request.Scope = scope
	if len(request.Targets) == 0 {
		request.Targets = nil
		return request, nil
	}

	carrier, ok := extensionAuthoringCarrierForTarget(request.Targets[0])
	if !ok {
		return RemoveExtensionRequest{}, fmt.Errorf(
			"extension removal does not support --target %s; supported targets are %s",
			request.Targets[0],
			extensionAuthoringTargetSummary(),
		)
	}
	admittedTarget, _ := carrier.AdmittedTarget()
	if scope != "" && !carrier.AdmitsTargetScope(admittedTarget, target.Scope(scope)) {
		return RemoveExtensionRequest{}, fmt.Errorf(
			"--target %s supports only --scope %s",
			admittedTarget,
			strings.Join(extensionAuthoringScopeNames(carrier), " or "),
		)
	}
	request.Targets = []string{string(admittedTarget)}
	return request, nil
}

func extensionAuthoringScope(carrier desiredextension.Carrier, rawScope string) (string, error) {
	admittedTarget, ok := carrier.AdmittedTarget()
	if !ok {
		return "", fmt.Errorf("unsupported extension carrier %q", carrier)
	}
	scope := strings.TrimSpace(rawScope)
	if scope == "" {
		if carrier.AdmitsTargetScope(admittedTarget, target.ScopeProject) {
			return string(target.ScopeProject), nil
		}
		return "", fmt.Errorf("--scope global is required for --target %s", admittedTarget)
	}
	if !carrier.AdmitsTargetScope(admittedTarget, target.Scope(scope)) {
		return "", fmt.Errorf(
			"--target %s supports only --scope %s",
			admittedTarget,
			strings.Join(extensionAuthoringScopeNames(carrier), " or "),
		)
	}
	return scope, nil
}

func extensionAuthoringSource(carrier desiredextension.Carrier, value string) (declaration.ExtensionSource, error) {
	sourceKind, ok := carrier.RequiredSourceKind()
	if !ok {
		return declaration.ExtensionSource{}, fmt.Errorf("unsupported extension carrier %q", carrier)
	}
	switch sourceKind {
	case desiredextension.SourceKindMarketplace:
		if strings.TrimSpace(value) == "" {
			return declaration.ExtensionSource{}, fmt.Errorf("extension source must be PLUGIN@MARKETPLACE for %s", extensionAuthoringSourceTargetOptions(sourceKind))
		}
		if value != strings.TrimSpace(value) {
			return declaration.ExtensionSource{}, fmt.Errorf("extension source must not contain leading or trailing whitespace")
		}
		source, err := desiredextension.NewSourceRef(sourceKind, value)
		if err != nil {
			if strings.Contains(err.Error(), "marketplace source must be PLUGIN@MARKETPLACE") {
				return declaration.ExtensionSource{}, fmt.Errorf("extension source must be PLUGIN@MARKETPLACE for %s", extensionAuthoringSourceTargetOptions(sourceKind))
			}
			return declaration.ExtensionSource{}, err
		}
		return declaration.ExtensionSource{Marketplace: source.Ref()}, nil
	case desiredextension.SourceKindHostSource:
		if strings.TrimSpace(value) == "" {
			return declaration.ExtensionSource{}, fmt.Errorf("extension source is required")
		}
		if value != strings.TrimSpace(value) {
			return declaration.ExtensionSource{}, fmt.Errorf("extension source must not contain leading or trailing whitespace")
		}
		source, err := desiredextension.NewSourceRef(sourceKind, value)
		if err != nil {
			return declaration.ExtensionSource{}, err
		}
		return declaration.ExtensionSource{HostSource: source.Ref()}, nil
	default:
		admittedTarget, _ := carrier.AdmittedTarget()
		return declaration.ExtensionSource{}, fmt.Errorf("--target %s has unsupported extension source kind %q", admittedTarget, sourceKind)
	}
}

func extensionAuthoringScopeNames(carrier desiredextension.Carrier) []string {
	names := make([]string, 0, 2)
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		admittedTarget, ok := carrier.AdmittedTarget()
		if ok && carrier.AdmitsTargetScope(admittedTarget, scope) {
			names = append(names, string(scope))
		}
	}
	return names
}

func extensionAuthoringLabel(carrier desiredextension.Carrier) string {
	label, ok := carrier.Label()
	if !ok {
		return string(carrier)
	}
	return label
}

func extensionAuthoringSourceTargetOptions(sourceKind desiredextension.SourceKind) string {
	values := make([]string, 0)
	for _, carrier := range desiredextension.SupportedCarriers() {
		requiredKind, sourceOK := carrier.RequiredSourceKind()
		admittedTarget, targetOK := carrier.AdmittedTarget()
		if sourceOK && targetOK && requiredKind == sourceKind {
			values = append(values, string(admittedTarget))
		}
	}
	return "--target " + strings.Join(values, " or ")
}

func extensionCanonicalSourceRef(extension declaration.Extension, carrier desiredextension.Carrier, context string) (desiredextension.SourceRef, error) {
	sourceKind, ok := carrier.RequiredSourceKind()
	if !ok {
		return desiredextension.SourceRef{}, fmt.Errorf("%s.source: unsupported extension carrier %q", context, carrier)
	}
	label := extensionAuthoringLabel(carrier)
	var value string
	switch sourceKind {
	case desiredextension.SourceKindMarketplace:
		if extension.Source.HostSource != "" {
			return desiredextension.SourceRef{}, fmt.Errorf("%s.source.host_source: %s supports source.marketplace, not source.host_source", context, label)
		}
		if extension.Source.Marketplace == "" {
			return desiredextension.SourceRef{}, fmt.Errorf("%s.source.marketplace: marketplace is required", context)
		}
		value = extension.Source.Marketplace
	case desiredextension.SourceKindHostSource:
		if extension.Source.Marketplace != "" {
			return desiredextension.SourceRef{}, fmt.Errorf("%s.source.marketplace: %s supports source.host_source, not source.marketplace", context, label)
		}
		if extension.Source.HostSource == "" {
			return desiredextension.SourceRef{}, fmt.Errorf("%s.source.host_source: host_source is required", context)
		}
		value = extension.Source.HostSource
	default:
		return desiredextension.SourceRef{}, fmt.Errorf("%s.source: unsupported extension source kind %q", context, sourceKind)
	}
	source, err := desiredextension.NewSourceRef(sourceKind, value)
	if err != nil {
		return desiredextension.SourceRef{}, fmt.Errorf("%s.source: %w", context, err)
	}
	return source, nil
}
