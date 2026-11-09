package record

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

func newNative16Value(record nativeJournalRecord, owner stateauthority.Authority, before, after durable.Snapshot) (Native, error) {
	namespace, documents, err := lowerNative16Namespace(record)
	if err != nil {
		return Native{}, err
	}
	body, envelope := lowerNative16Record(record, documents)
	return Native{record: body, legacy: envelope, namespace: namespace, owner: owner, before: before, after: after}, nil
}

func lowerNative16Namespace(record nativeJournalRecord) (nativeNamespaceFacts, []documentNamespaceFacts, error) {
	reference, err := native16ManifestRootReference()
	if err != nil {
		return nativeNamespaceFacts{}, nil, err
	}
	manifest, err := record.ManifestRootProvenance.canonical()
	if err != nil {
		return nativeNamespaceFacts{}, nil, err
	}
	result := nativeNamespaceFacts{
		claims:            journaldocument.LegacyGlobalClaims,
		manifestReference: reference,
		roots:             map[output.RootReference]rootedpath.AuthorityProvenance{reference: manifest},
	}
	documents := make([]documentNamespaceFacts, len(record.DocumentGroups))
	for index, group := range record.DocumentGroups {
		facts, err := lowerNative16DocumentNamespace(group, record.ManifestRootProvenance)
		if err != nil {
			return nativeNamespaceFacts{}, nil, err
		}
		for _, alias := range facts.aliases {
			reference := alias.location.RootReference()
			if previous, known := result.roots[reference]; known && previous != alias.root {
				return nativeNamespaceFacts{}, nil, fmt.Errorf("native reference has inconsistent recorded root anchors")
			}
			result.roots[reference] = alias.root
		}
		documents[index] = facts
	}
	return result, documents, nil
}

func lowerNative16DocumentNamespace(group recoveryDocumentGroup, manifest recoveryRootProvenance) (documentNamespaceFacts, error) {
	provenance, err := group.Writer.Root.canonical()
	if err != nil {
		return documentNamespaceFacts{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(group.Writer.RelativePath)
	if err != nil || relative.Path() != group.Writer.RelativePath {
		return documentNamespaceFacts{}, fmt.Errorf("recorded writer relative referent is invalid")
	}
	writer, err := journalnamespace.NewWriterPin(provenance, relative)
	if err != nil {
		return documentNamespaceFacts{}, err
	}
	result := documentNamespaceFacts{writer: writer, aliases: make([]documentAliasFacts, len(group.Aliases))}
	for index, alias := range group.Aliases {
		address, err := alias.Address.canonical()
		if err != nil {
			return documentNamespaceFacts{}, err
		}
		location, err := native16RootBindingForAlias(address)
		if err != nil {
			return documentNamespaceFacts{}, err
		}
		if err := alias.LogicalRoot.validate(address.Scope(), manifest); err != nil {
			return documentNamespaceFacts{}, err
		}
		root, err := alias.LogicalRoot.Provenance.canonical()
		if err != nil {
			return documentNamespaceFacts{}, err
		}
		result.aliases[index] = documentAliasFacts{
			address: address, location: location, root: root,
			selected: alias.SelectedObservation, before: alias.ObservedBefore,
		}
	}
	return result, nil
}
