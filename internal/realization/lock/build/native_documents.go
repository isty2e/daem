package build

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

type nativeDocumentInput struct {
	config    hostconfig.Config
	placement profile.HostConfigPlacement
}

// BuildNativeDocumentsWithOptions selects the native lock generation for document-mode sources.
// This does not select a host executor or activate native key-mode construction.
func BuildNativeDocumentsWithOptions(ctx context.Context, environment desired.Environment, resolver acquisition.Resolver, options Options, limits configdocument.Limits) (lock.File, error) {
	if ctx == nil {
		return lock.File{}, fmt.Errorf("native document build context is required")
	}
	if err := ctx.Err(); err != nil {
		return lock.File{}, err
	}
	if err := environment.Validate(); err != nil {
		return lock.File{}, fmt.Errorf("desired environment: %w", err)
	}
	if err := limits.Validate(); err != nil {
		return lock.File{}, err
	}
	documents := make([]nativeDocumentInput, 0, len(environment.HostConfigs()))
	for _, config := range environment.HostConfigs() {
		if err := ctx.Err(); err != nil {
			return lock.File{}, err
		}
		if config.Ownership() != hostconfig.OwnershipDocument {
			return lock.File{}, fmt.Errorf("host configuration %q: native key source admission is not implemented", config.ID())
		}
		placement, err := profile.ResolveHostConfigPlacement(profile.HostConfigPlacementInput{
			Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
		})
		if err != nil {
			return lock.File{}, err
		}
		documents = append(documents, nativeDocumentInput{config: config, placement: placement})
	}
	return buildWithProfile(ctx, environment, resolver, options, documents, limits, lockBuildProfile{
		version: lock.NativeVersion, section: lock.NewNativeLockedSection, validate: lock.ValidateNative,
	})
}

func lockResolvedNativeDocuments(ctx context.Context, documents []nativeDocumentInput, results []sourceTaskResult, limits configdocument.Limits, options Options) ([]lock.LockedSubjectContract, error) {
	if len(documents) != len(results) {
		return nil, fmt.Errorf("native document resolution count does not match declarations")
	}
	contracts := make([]lock.LockedSubjectContract, 0, 2*len(documents))
	for index, document := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result := results[index]
		resource, projection, err := lockResolvedNativeDocument(ctx, document, result, limits)
		if err != nil {
			options.Events.Emit(result.task.event(EventResourceLockFailed, err))
			return nil, fmt.Errorf("lock host_config %q: %w", document.config.ID(), err)
		}
		contracts = append(contracts, resource, projection)
		options.Events.Emit(result.task.event(EventResourceLocked, nil))
	}
	return contracts, nil
}

func lockResolvedNativeDocument(ctx context.Context, document nativeDocumentInput, result sourceTaskResult, limits configdocument.Limits) (lock.LockedSubjectContract, lock.LockedSubjectContract, error) {
	var zero lock.LockedSubjectContract
	input, err := resolvedArtifactInputFromSourceTaskResult(result)
	if err != nil {
		return zero, zero, err
	}
	identity, view, err := input.exactArtifact(document.config.ID())
	if err != nil {
		return zero, zero, err
	}
	content, err := directfile.ReadExact(ctx, view, identity)
	if err != nil {
		return zero, zero, err
	}
	bytes := content.Bytes()
	if document.placement.Grammar() != profile.HostConfigOpaque {
		syntax, err := configdocument.ParseSyntax(string(document.placement.Grammar()))
		if err != nil {
			return zero, zero, err
		}
		if _, err := configdocument.Parse(ctx, bytes, syntax, limits); err != nil {
			return zero, zero, err
		}
	}
	materialization, err := artifact.NewFileMaterialization(identity, bytes, content.Mode().Perm()&0o111 != 0, false)
	if err != nil {
		return zero, zero, err
	}
	use, err := lock.NewExactFileUse(document.placement.Scope(), false)
	if err != nil {
		return zero, zero, err
	}
	derivation, err := lock.NewFileMaterializationDerivation(materialization)
	if err != nil {
		return zero, zero, err
	}
	subject, err := resourcetopology.Subject(document.config.ID())
	if err != nil {
		return zero, zero, err
	}
	resource, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID: document.config.ID(), SubjectID: subject, ExactSupply: identity, ExactFileUse: &use,
		HostConfigPlacement: &document.placement, Derivation: derivation,
	})
	if err != nil {
		return zero, zero, err
	}
	if err := resource.ValidateFileMaterialization(materialization); err != nil {
		return zero, zero, err
	}
	projection, err := lock.NewHostConfigDocumentSubjectContract(document.config.ID(), document.placement)
	if err != nil {
		return zero, zero, err
	}
	return resource, projection, nil
}
