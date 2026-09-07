package profile

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
)

const (
	hookAssetProjectPlacementID = topologyhook.AssetProjectProjectionNamespace
	hookAssetGlobalPlacementID  = topologyhook.AssetGlobalProjectionNamespace
	hookAssetProjectRoot        = ".daem/hook-assets"
	hookAssetGlobalRoot         = "@data/hook-assets"
	hookAssetAdapterContract    = "managed-hook-asset-file-v1"
	hookAssetWriteRoute         = "managed-hook-asset-file.write"
	hookAssetRemoveRoute        = "managed-path.remove"
	hookAssetFileName           = "asset"
)

// HookAssetPlacement is the static content-addressed file placement selected
// for one referenced HookAsset. It owns no route, source, or mutation fact.
type HookAssetPlacement struct {
	id              string
	consumerTargets []target.Target
	scope           target.Scope
	root            output.Destination
}

// HookAssetPlacementFor selects the one admitted daem-owned asset root.
func HookAssetPlacementFor(scope target.Scope, consumers []target.Target) (HookAssetPlacement, error) {
	parsedScope, err := target.ParseScope(string(scope))
	if err != nil {
		return HookAssetPlacement{}, err
	}
	canonicalConsumers, err := target.CanonicalSet(consumers)
	if err != nil {
		return HookAssetPlacement{}, fmt.Errorf("hook asset placement consumer targets: %w", err)
	}
	if len(canonicalConsumers) == 0 {
		return HookAssetPlacement{}, fmt.Errorf("hook asset placement requires at least one consumer target")
	}
	for _, consumer := range canonicalConsumers {
		if !TargetSupports(consumer, entity.KindHook) {
			return HookAssetPlacement{}, fmt.Errorf("target %q does not admit HookAsset path projection", consumer)
		}
	}

	placement := HookAssetPlacement{consumerTargets: canonicalConsumers, scope: parsedScope}
	var rootText string
	if parsedScope == target.ScopeProject {
		placement.id = hookAssetProjectPlacementID
		rootText = hookAssetProjectRoot
	} else {
		placement.id = hookAssetGlobalPlacementID
		rootText = hookAssetGlobalRoot
	}
	placement.root, err = output.Parse(rootText)
	if err != nil {
		return HookAssetPlacement{}, err
	}
	if err := placement.Validate(); err != nil {
		return HookAssetPlacement{}, err
	}
	return placement, nil
}

// ImplementedHookAssetPlacements returns project and global physical placement
// facts with the complete canonical set of direct Hook consumer targets.
func ImplementedHookAssetPlacements() []HookAssetPlacement {
	consumers := make([]target.Target, 0)
	for _, selectedTarget := range target.SupportedTargets() {
		if TargetSupports(selectedTarget, entity.KindHook) {
			consumers = append(consumers, selectedTarget)
		}
	}
	result := make([]HookAssetPlacement, 0, 2)
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		placement, err := HookAssetPlacementFor(scope, consumers)
		if err != nil {
			panic(err)
		}
		result = append(result, placement)
	}
	return result
}

// Validate rejects forged or partially initialized placement values.
func (placement HookAssetPlacement) Validate() error {
	if err := validateProfileToken("HookAsset placement id", placement.id); err != nil {
		return err
	}
	if err := validateTargetSet(placement.consumerTargets); err != nil {
		return err
	}
	if _, err := target.ParseScope(string(placement.scope)); err != nil {
		return err
	}
	if err := placement.root.ValidateScope(placement.scope); err != nil {
		return err
	}
	wantID, wantRoot := hookAssetGlobalPlacementID, hookAssetGlobalRoot
	if placement.scope == target.ScopeProject {
		wantID, wantRoot = hookAssetProjectPlacementID, hookAssetProjectRoot
	}
	if placement.id != wantID || placement.root.String() != wantRoot {
		return fmt.Errorf("HookAsset placement does not match scope %q", placement.scope)
	}
	return nil
}

// Destination returns the portable content-addressed file destination.
func (placement HookAssetPlacement) Destination(assetName string, contentHash artifact.ContentHash) (output.Destination, error) {
	if err := placement.Validate(); err != nil {
		return output.Destination{}, err
	}
	if err := validatePathComponent("HookAsset name", assetName); err != nil {
		return output.Destination{}, err
	}
	if err := contentHash.Validate(); err != nil {
		return output.Destination{}, err
	}
	hashSegment := strings.ReplaceAll(string(contentHash), ":", "-")
	destination, err := output.Parse(path.Join(placement.root.String(), assetName, hashSegment, hookAssetFileName))
	if err != nil {
		return output.Destination{}, err
	}
	if err := destination.ValidateScope(placement.scope); err != nil {
		return output.Destination{}, err
	}
	return destination, nil
}

// ContentHash inverts one canonical asset destination for this placement.
func (placement HookAssetPlacement) ContentHash(assetName string, destination output.Destination) (artifact.ContentHash, error) {
	if err := placement.Validate(); err != nil {
		return "", err
	}
	if err := validatePathComponent("HookAsset name", assetName); err != nil {
		return "", err
	}
	destinationText := destination.String()
	prefix := path.Join(placement.root.String(), assetName) + "/"
	if !strings.HasPrefix(destinationText, prefix) {
		return "", fmt.Errorf("HookAsset destination %q is outside placement %q", destination, placement.id)
	}
	relative := strings.TrimPrefix(destinationText, prefix)
	parts := strings.Split(relative, "/")
	if len(parts) != 2 || parts[1] != hookAssetFileName {
		return "", fmt.Errorf("HookAsset destination %q has invalid content-addressed shape", destination)
	}
	algorithm, digest, ok := strings.Cut(parts[0], "-")
	if !ok {
		return "", fmt.Errorf("HookAsset destination %q has invalid hash segment", destination)
	}
	contentHash := artifact.ContentHash(algorithm + ":" + digest)
	if err := contentHash.Validate(); err != nil {
		return "", err
	}
	canonical, err := placement.Destination(assetName, contentHash)
	if err != nil {
		return "", err
	}
	if canonical != destination {
		return "", fmt.Errorf("HookAsset destination %q is not canonical", destination)
	}
	return contentHash, nil
}

// ExactPermissionMode returns the family-owned publish mode for one HookAsset executable class.
func (placement HookAssetPlacement) ExactPermissionMode(executable bool) (realization.ExactPathPermissionMode, error) {
	if err := placement.Validate(); err != nil {
		return realization.ExactPathPermissionMode{}, err
	}
	fileMode := os.FileMode(0o600)
	if executable {
		fileMode = 0o700
	}
	return realization.NewExactPathPermissionMode(fileMode)
}

// Realize constructs the managed-file path realization for one exact hash.
func (placement HookAssetPlacement) Realize(
	assetName string,
	contentHash artifact.ContentHash,
	executable bool,
	writeRoute OperationRoute,
) (realization.RealizationSpec, error) {
	if err := validateHookAssetRoute(placement, writeRoute, OperationWrite); err != nil {
		return realization.RealizationSpec{}, err
	}
	destination, err := placement.Destination(assetName, contentHash)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	exactMode, err := placement.ExactPermissionMode(executable)
	if err != nil {
		return realization.RealizationSpec{}, err
	}
	return realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID:            placement.id,
		ConsumerTargets:        placement.consumerTargets,
		Scope:                  placement.scope,
		Destination:            destination,
		ContentKind:            realization.PathProjectionFile,
		PlacementMode:          realization.PathProjectionCopy,
		PermissionPolicy:       realization.PathPermissionsExact,
		ExactPermissionMode:    exactMode,
		AdapterContractVersion: writeRoute.AdapterContractVersion(),
	})
}

func (placement HookAssetPlacement) ID() string          { return placement.id }
func (placement HookAssetPlacement) Scope() target.Scope { return placement.scope }
func (placement HookAssetPlacement) Root() output.Destination {
	return placement.root
}

func (placement HookAssetPlacement) ConsumerTargets() []target.Target {
	return append([]target.Target(nil), placement.consumerTargets...)
}

func hookAssetOperationRoutes() []OperationRoute {
	result := make([]OperationRoute, 0, 4)
	for _, placementID := range []string{hookAssetProjectPlacementID, hookAssetGlobalPlacementID} {
		result = append(
			result,
			mustOperationRoute(entity.KindHookAsset, OperationWrite, placementID, hookAssetWriteRoute, hookAssetAdapterContract),
			mustOperationRoute(entity.KindHookAsset, OperationRemove, placementID, hookAssetRemoveRoute, hookAssetAdapterContract),
		)
	}
	return result
}

func validateHookAssetRoute(placement HookAssetPlacement, route OperationRoute, operation Operation) error {
	if err := route.Validate(); err != nil {
		return err
	}
	if !route.Correlates(entity.KindHookAsset, placement.ID(), operation) {
		return fmt.Errorf(
			"operation route %q/%q does not correlate with HookAsset placement %q",
			route.Operation(),
			route.RouteID(),
			placement.ID(),
		)
	}
	return nil
}

func validatePathComponent(label string, value string) error {
	if strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value ||
		value == "." || value == ".." || strings.ContainsAny(value, `/\`) || path.Clean(value) != value {
		return fmt.Errorf("%s %q must be one canonical path component", label, value)
	}
	return nil
}
