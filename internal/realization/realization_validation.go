package realization

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// Validate rejects zero, mixed, and malformed realization variants.
func (spec RealizationSpec) Validate() error {
	bodyCount := 0
	if spec.pathProjection != nil {
		bodyCount++
	}
	if spec.aggregateContribution != nil {
		bodyCount++
	}
	if spec.delegatedRelation != nil {
		bodyCount++
	}
	if bodyCount != 1 {
		return fmt.Errorf("realization %q must contain exactly one body", spec.kind)
	}

	switch spec.kind {
	case RealizationManagedPathProjection:
		if spec.pathProjection == nil {
			return fmt.Errorf("managed path projection body is required")
		}
		return spec.pathProjection.validate()
	case RealizationManagedAggregateContribution:
		if spec.aggregateContribution == nil {
			return fmt.Errorf("managed aggregate contribution body is required")
		}
		return spec.aggregateContribution.Validate()
	case RealizationDelegatedRelation:
		if spec.delegatedRelation == nil {
			return fmt.Errorf("delegated relation body is required")
		}
		return spec.delegatedRelation.validate()
	default:
		return fmt.Errorf("unsupported realization kind %q", spec.kind)
	}
}

func (projection ManagedPathProjection) validate() error {
	if err := validateRealizationToken("placement id", projection.placementID); err != nil {
		return fmt.Errorf("managed path projection: %w", err)
	}
	if err := validateRealizationTargetSet(projection.consumerTargets); err != nil {
		return fmt.Errorf("managed path projection: %w", err)
	}
	if _, err := target.ParseScope(string(projection.scope)); err != nil {
		return fmt.Errorf("managed path projection: %w", err)
	}
	if err := validateRealizationToken("adapter contract version", projection.adapterContractVersion); err != nil {
		return fmt.Errorf("managed path projection: %w", err)
	}
	if err := projection.destination.ValidateScope(projection.scope); err != nil {
		return fmt.Errorf("managed path projection: %w", err)
	}
	switch projection.contentKind {
	case PathProjectionFile, PathProjectionDirectory:
	default:
		return fmt.Errorf("managed path projection content kind %q is unsupported", projection.contentKind)
	}
	switch projection.placementMode {
	case PathProjectionCopy, PathProjectionSymlink, PathProjectionHardlink:
	default:
		return fmt.Errorf("managed path projection placement mode %q is unsupported", projection.placementMode)
	}
	switch {
	case projection.contentKind == PathProjectionFile && projection.placementMode == PathProjectionCopy:
		if projection.permissionPolicy != PathPermissionsExecutableClass &&
			projection.permissionPolicy != PathPermissionsExact {
			return fmt.Errorf(
				"managed copied file permission policy %q is unsupported",
				projection.permissionPolicy,
			)
		}
		if projection.permissionPolicy == PathPermissionsExact {
			if err := projection.exactPermissionMode.Validate(); err != nil {
				return fmt.Errorf("managed copied file: %w", err)
			}
		} else if !projection.exactPermissionMode.isZero() {
			return fmt.Errorf("executable-class managed file must not carry an exact permission mode")
		}
	case projection.permissionPolicy != PathPermissionsNone:
		return fmt.Errorf(
			"managed path content kind %q mode %q requires permission policy %q",
			projection.contentKind,
			projection.placementMode,
			PathPermissionsNone,
		)
	case !projection.exactPermissionMode.isZero():
		return fmt.Errorf("managed path without exact permission policy must not carry an exact permission mode")
	}
	return nil
}

func validateRealizationTargetSet(values []target.Target) error {
	if len(values) == 0 {
		return fmt.Errorf("at least one consumer target is required")
	}
	canonical, err := target.CanonicalSet(values)
	if err != nil {
		return fmt.Errorf("consumer targets: %w", err)
	}
	if !slices.Equal(values, canonical) {
		return fmt.Errorf("consumer targets are not canonical")
	}
	return nil
}

func (relation DelegatedRelation) validate() error {
	if err := validateRealizationPlacement(relation.placementID, relation.target, relation.scope, relation.routeRequest.ContractVersion()); err != nil {
		return fmt.Errorf("delegated relation: %w", err)
	}
	if err := validateRealizationText("source namespace", relation.sourceNamespace, false); err != nil {
		return fmt.Errorf("delegated relation: %w", err)
	}
	if err := relation.expectedRelation.Validate(); err != nil {
		return fmt.Errorf("delegated relation: %w", err)
	}
	if err := relation.routeRequest.Validate(); err != nil {
		return fmt.Errorf("delegated relation: %w", err)
	}
	return validateRealizationStringSet(relation.verifiedRelationFields, "verified relation field")
}

func validateRealizationPlacement(id string, selectedTarget target.Target, scope target.Scope, adapterVersion string) error {
	if err := validateRealizationToken("placement id", id); err != nil {
		return err
	}
	if _, err := target.ParseTarget(string(selectedTarget)); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	if _, err := target.ParseScope(string(scope)); err != nil {
		return fmt.Errorf("scope: %w", err)
	}
	return validateRealizationToken("adapter contract version", adapterVersion)
}

func validateRealizationToken(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be a stable token", label)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'A' && character <= 'Z') ||
			(character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%s must be a stable token", label)
	}
	return nil
}

func validateRealizationText(label string, value string, allowEmpty bool) error {
	if value == "" && allowEmpty {
		return nil
	}
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must be non-empty and trimmed", label)
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", label)
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.Is(unicode.Bidi_Control, character)
	}) >= 0 {
		return fmt.Errorf("%s must not contain control characters", label)
	}
	return nil
}

func validatePortableRealizationPath(label string, value string) error {
	_, err := output.Parse(value)
	if err != nil {
		if label == "destination" {
			return err
		}
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

func validateRealizationPathScope(label string, value string, scope target.Scope) error {
	destination, err := output.Parse(value)
	if err != nil {
		if label == "destination" {
			return err
		}
		return fmt.Errorf("%s: %w", label, err)
	}
	if err := destination.ValidateScope(scope); err != nil {
		if label == "destination" {
			return err
		}
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

func validateSHA256Identity(label string, value string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return fmt.Errorf("%s must be a lowercase sha256 digest", label)
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return fmt.Errorf("%s must be a lowercase sha256 digest", label)
		}
	}
	return nil
}

func validateRealizationStringSet(values []string, label string) error {
	if len(values) == 0 {
		return fmt.Errorf("%s values are required", label)
	}
	for index, value := range values {
		if err := validateRealizationToken(label, value); err != nil {
			return err
		}
		if index > 0 && values[index-1] >= value {
			return fmt.Errorf("%s values must be sorted and unique", label)
		}
	}
	return nil
}

func canonicalStringSet(values []string) []string {
	result := append([]string(nil), values...)
	for index := range result {
		result[index] = strings.TrimSpace(result[index])
	}
	sort.Strings(result)
	unique := result[:0]
	for _, value := range result {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	return unique
}
