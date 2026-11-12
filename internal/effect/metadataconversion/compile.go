package metadataconversion

import (
	"bytes"
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

type compiledTarget struct {
	target      fileset.PublicationTarget
	materialize fileset.PublicationMaterializer
}

func compileState(ctx context.Context, path string, entry sourceEntry, reference pathauthority.Reference) (compiledTarget, error) {
	if !entry.image.Exists() {
		target, err := fileset.NewPublicationTarget(path, entry.image, entry.image)
		return compiledTarget{target: target}, err
	}
	materialize := func(ctx context.Context, before []byte) ([]byte, error) {
		snapshot, recorded, err := stateFacts(ctx, before)
		if err != nil {
			return nil, err
		}
		if !recorded.IsZero() && recorded.Identity().Equal(reference.Identity()) && !recorded.Equal(reference) {
			return nil, fmt.Errorf("statefile requires an explicit recorded-locator replacement")
		}
		image, err := statefile.NewStateImage(snapshot, reference)
		if err != nil {
			return nil, err
		}
		if recorded.Equal(reference) {
			return slices.Clone(before), nil
		}
		return statefile.MarshalImage(image)
	}
	return compileTarget(ctx, path, entry, materialize)
}

func compileRegistry(ctx context.Context, path string, kind sourceKind, entry sourceEntry, table pathauthority.ReferenceTable) (compiledTarget, error) {
	if !entry.image.Exists() {
		target, err := fileset.NewPublicationTarget(path, entry.image, entry.image)
		return compiledTarget{target: target}, err
	}
	beforeImage := entry.image
	materialize := func(ctx context.Context, before []byte) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := sourceEntry{image: beforeImage, content: before}
		switch kind {
		case ownershipSource:
			registry, recorded, err := ownershipFacts(source)
			if err != nil {
				return nil, err
			}
			references, err := requiredReferences(table, ownershipstore.RequiredReferenceIdentities(registry))
			if err != nil {
				return nil, err
			}
			image, err := ownershipstore.NewRegistryImage(registry, references)
			if err != nil {
				return nil, err
			}
			if recorded != nil {
				return slices.Clone(before), nil
			}
			return ownershipstore.MarshalImage(image)
		case carrierSource:
			registry, recorded, err := carrierFacts(source)
			if err != nil {
				return nil, err
			}
			references, err := requiredReferences(table, carrierclaim.RequiredReferenceIdentities(registry))
			if err != nil {
				return nil, err
			}
			image, err := carrierclaim.NewRegistryImage(registry, references)
			if err != nil {
				return nil, err
			}
			if recorded != nil {
				return slices.Clone(before), nil
			}
			return carrierclaim.MarshalImage(image)
		default:
			return nil, fmt.Errorf("unsupported conversion registry role %q", kind)
		}
	}
	return compileTarget(ctx, path, entry, materialize)
}

func compileTarget(ctx context.Context, path string, entry sourceEntry, materialize fileset.PublicationMaterializer) (compiledTarget, error) {
	after, err := materialize(ctx, entry.content)
	if err != nil {
		return compiledTarget{}, err
	}
	if err := ctx.Err(); err != nil {
		return compiledTarget{}, err
	}
	mode := entry.image.Mode()
	if !bytes.Equal(after, entry.content) {
		mode = 0o600
	}
	image, err := fileset.NewPublicationImage(after, mode)
	if err != nil {
		return compiledTarget{}, err
	}
	target, err := fileset.NewPublicationTarget(path, entry.image, image)
	if err != nil {
		return compiledTarget{}, err
	}
	compiled := compiledTarget{target: target}
	if target.Writes() {
		compiled.materialize = materialize
	}
	return compiled, nil
}

func requiredReferences(table pathauthority.ReferenceTable, identities []pathauthority.Exact) ([]pathauthority.Reference, error) {
	references := make([]pathauthority.Reference, 0, len(identities))
	for _, identity := range identities {
		reference, present := table.Reference(identity)
		if !present {
			return nil, fmt.Errorf("metadata identity %q lacks a qualified reference", identity.Key())
		}
		references = append(references, reference)
	}
	return references, nil
}
