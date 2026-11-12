package carrierclaim

import (
	"context"
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

type registryDocument struct {
	registry   durablecarrier.GlobalCarrierClaims
	references pathauthority.ReferenceTable
}

// PrepareRewrite prepares a FileSet successor without publishing it. The caller
// must hold the complete leases and recheck its physical revisions afterward.
func (store Store) PrepareRewrite(ctx context.Context, expected, next durablecarrier.GlobalCarrierClaims, references ...pathauthority.Reference) ([]byte, error) {
	document, _, _, err := store.loadForCommit(ctx)
	if err != nil {
		return nil, err
	}
	if !document.registry.Equal(expected) {
		return nil, fmt.Errorf("carrier registry changed before rewrite preparation: %w", mutation.StaleSnapshotError{})
	}
	content, err := document.encodeSuccessor(ctx, next, references)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func decodeRegistryDocument(content []byte) (registryDocument, error) {
	if len(content) > maximumRegistryBytes {
		return registryDocument{}, fmt.Errorf("carrier claim registry exceeds %d bytes", maximumRegistryBytes)
	}
	version, err := jsonstrict.ValidateVersionedObject(content, "carrier claim registry", maximumRegistryJSONDepth)
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
	return identity.Key()
}

func (document registryDocument) encodeSuccessor(ctx context.Context, next durablecarrier.GlobalCarrierClaims, candidates []pathauthority.Reference) ([]byte, error) {
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
			return nil, fmt.Errorf("carrier reference %q is outside the successor", reference.Identity().Key())
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
		observed, err := mutation.ObservePersistedDirectoryEntryAuthority(reference.Locator())
		if err != nil {
			return nil, fmt.Errorf("qualify added carrier reference %q: %w", reference.Locator(), err)
		}
		if !observed.Exact().Equal(reference.Identity()) {
			return nil, fmt.Errorf("added carrier reference %q does not identify its recorded namespace", reference.Locator())
		}
	}
	return MarshalImage(RegistryImage{registry: next, references: table})
}
