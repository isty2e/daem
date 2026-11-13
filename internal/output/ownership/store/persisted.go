package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output/ownership"
)

// registryDocument keeps located evidence with the facts read for one update.
// An uninitialized table denotes a legacy document, not inferred references.
type registryDocument struct {
	registry   ownership.Registry
	references pathauthority.ReferenceTable
}

func emptyLocatedRegistryDocument() registryDocument {
	return registryDocument{registry: ownership.EmptyRegistry(), references: pathauthority.EmptyReferenceTable()}
}

// PrepareRewrite prepares a FileSet successor without publishing it. The caller
// must hold the complete leases and recheck its physical revisions afterward.
func (store Store) PrepareRewrite(ctx context.Context, expected, next ownership.Registry, references ...pathauthority.Reference) (content []byte, returnErr error) {
	if ctx == nil {
		return nil, fmt.Errorf("ownership rewrite context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	observations, err := store.newPathAuthorityObservationSession()
	if err != nil {
		return nil, err
	}
	document, _, _, capability, err := store.loadForUpdate(ctx, nil, observations)
	if err != nil {
		return nil, err
	}
	defer func() {
		if capability != nil {
			returnErr = errors.Join(returnErr, capability.Close())
		}
		returnErr = errors.Join(returnErr, ctx.Err())
		if returnErr != nil {
			content = nil
		}
	}()
	if !document.registry.Equal(expected) {
		return nil, fmt.Errorf("ownership registry changed before rewrite preparation: %w", mutation.StaleSnapshotError{})
	}
	return document.encodeSuccessor(ctx, next, references, observations)
}

func decodeRegistryDocument(content []byte) (registryDocument, error) {
	if int64(len(content)) > maximumOwnershipRegistryBytes {
		return registryDocument{}, fmt.Errorf("ownership registry exceeds %d bytes", maximumOwnershipRegistryBytes)
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "ownership registry", maximumOwnershipRegistryJSONDepth)
	if err != nil {
		return registryDocument{}, err
	}
	if version == locatedRegistryVersion {
		image, err := UnmarshalImage(content)
		if err != nil {
			return registryDocument{}, err
		}
		return registryDocument{registry: image.registry, references: image.references}, nil
	}
	registry, err := decode(content)
	if err != nil {
		return registryDocument{}, err
	}
	return registryDocument{registry: registry}, nil
}

func (document registryDocument) locator(identity pathauthority.Exact) string {
	if reference, present := document.references.Reference(identity); present {
		return reference.Locator()
	}
	return identity.Key() // Only legacy input lacks a complete reference table.
}

func (document registryDocument) encodeSuccessor(ctx context.Context, next ownership.Registry, candidates []pathauthority.Reference, observations *pathAuthorityObservationSession) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if document.references.Validate() != nil {
		return encode(next)
	}

	identities := RequiredReferenceIdentities(next)
	required := make(map[pathauthority.Exact]bool, len(identities))
	for _, identity := range identities {
		_, retained := document.references.Reference(identity)
		required[identity] = !retained
	}
	var additions []pathauthority.Reference
	for _, reference := range candidates {
		if err := reference.Validate(); err != nil {
			return nil, err
		}
		added, member := required[reference.Identity()]
		if !member {
			return nil, fmt.Errorf("ownership reference %q is outside the successor", reference.Identity().Key())
		}
		if added {
			additions = append(additions, reference)
		}
	}
	table, err := document.references.Converge(identities, additions)
	if err != nil {
		return nil, err
	}
	for _, reference := range additions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		observed, err := observations.observePersisted(reference.Locator())
		if err != nil {
			return nil, fmt.Errorf("qualify added ownership reference %q: %w", reference.Locator(), err)
		}
		if !observed.Equal(reference.Identity()) {
			return nil, fmt.Errorf("added ownership reference %q does not identify its recorded namespace", reference.Locator())
		}
	}
	return MarshalImage(RegistryImage{registry: next, references: table})
}
