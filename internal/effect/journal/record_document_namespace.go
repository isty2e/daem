package journal

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

type recordedDocument struct {
	record      recoveryDocumentGroup
	owner       stateauthority.Authority
	operationID string
	initialized bool
}

type recordedDocumentAlias struct {
	record      recoveryDocumentAlias
	initialized bool
}

func (journal nativeJournalSnapshots) recordedDocuments() []recordedDocument {
	result := make([]recordedDocument, 0, len(journal.record.DocumentGroups))
	for _, record := range journal.record.DocumentGroups {
		result = append(result, journal.documentView(record))
	}
	return result
}

func (journal nativeJournalSnapshots) recordedDocumentAt(index int) (recordedDocument, error) {
	if index < 0 || index >= len(journal.record.DocumentGroups) {
		return recordedDocument{}, fmt.Errorf("recorded document index is outside the complete record")
	}
	return journal.documentView(journal.record.DocumentGroups[index]), nil
}

func (journal nativeJournalSnapshots) documentView(record recoveryDocumentGroup) recordedDocument {
	return recordedDocument{record: record, owner: journal.owner, operationID: journal.record.OperationID, initialized: true}
}

func (journal nativeJournalSnapshots) matchesManifestRoot(root rootedpath.AuthorityProvenance) bool {
	return persistedNativeRoot(root) == journal.record.ManifestRootProvenance
}

func (document recordedDocument) require() error {
	if !document.initialized {
		return fmt.Errorf("recorded document is required")
	}
	return nil
}

func (document recordedDocument) Addresses(ctx context.Context) ([]aggregate.DocumentAddress, error) {
	if err := document.require(); err != nil {
		return nil, err
	}
	aliases := make([]aggregate.DocumentAddress, 0, len(document.record.Aliases))
	for _, alias := range document.record.Aliases {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		address, err := alias.Address.canonical()
		if err != nil {
			return nil, err
		}
		aliases = append(aliases, address)
	}
	return aliases, nil
}

func (document recordedDocument) Writer() (rootedpath.AuthorityProvenance, rootedpath.RelativeDestination, error) {
	if err := document.require(); err != nil {
		return rootedpath.AuthorityProvenance{}, rootedpath.RelativeDestination{}, err
	}
	provenance, err := document.record.Writer.Root.canonical()
	if err != nil {
		return rootedpath.AuthorityProvenance{}, rootedpath.RelativeDestination{}, err
	}
	relative, err := rootedpath.NewRelativeDestination(document.record.Writer.RelativePath)
	if err != nil {
		return rootedpath.AuthorityProvenance{}, rootedpath.RelativeDestination{}, err
	}
	if relative.Path() != document.record.Writer.RelativePath {
		return rootedpath.AuthorityProvenance{}, rootedpath.RelativeDestination{}, fmt.Errorf("recorded writer relative referent is invalid")
	}
	return provenance, relative, nil
}

func (document recordedDocument) RequireCapturedNamespace(current mutation.DirectoryEntryAuthorityObservation) error {
	if err := document.require(); err != nil {
		return err
	}
	return matchRetainedNativeObservation(document.record.CapturedTarget, current)
}

func (document recordedDocument) Aliases() []recordedDocumentAlias {
	if !document.initialized {
		return nil
	}
	result := make([]recordedDocumentAlias, 0, len(document.record.Aliases))
	for _, alias := range document.record.Aliases {
		result = append(result, recordedDocumentAlias{record: alias, initialized: true})
	}
	return result
}

func (alias recordedDocumentAlias) Address() (aggregate.DocumentAddress, error) {
	if !alias.initialized {
		return aggregate.DocumentAddress{}, fmt.Errorf("recorded document alias is required")
	}
	return alias.record.Address.canonical()
}

func (alias recordedDocumentAlias) MatchesRoot(root rootedpath.AuthorityProvenance) bool {
	return alias.initialized && persistedNativeRoot(root) == alias.record.LogicalRoot.Provenance
}

func (alias recordedDocumentAlias) RequireObservations(current mutation.DirectoryEntryAuthorityObservation) error {
	if !alias.initialized {
		return fmt.Errorf("recorded document alias is required")
	}
	if err := matchRetainedNativeObservation(alias.record.SelectedObservation, current); err != nil {
		return err
	}
	return matchRetainedNativeObservation(alias.record.ObservedBefore, current)
}

func (document recordedDocument) PiQualification() (documentPiContext, bool, error) {
	if err := document.require(); err != nil {
		return documentPiContext{}, false, err
	}
	if document.record.PiOrder == nil {
		return documentPiContext{}, false, nil
	}
	facts, err := document.record.PiOrder.qualificationContext()
	return facts, err == nil, err
}

func (document recordedDocument) Removal() (recovery.DocumentRemoval, bool, error) {
	if err := document.require(); err != nil {
		return recovery.DocumentRemoval{}, false, err
	}
	if document.record.Removal == nil {
		return recovery.DocumentRemoval{}, false, nil
	}
	removal, err := document.record.Removal.canonical()
	return removal, err == nil, err
}

func (document recordedDocument) SelectedSubjects(ctx context.Context) ([]topology.SubjectID, error) {
	if err := document.require(); err != nil {
		return nil, err
	}
	_, subjects, err := document.record.canonicalReferences(ctx)
	return subjects, err
}

func (document recordedDocument) HasExactClaims() bool {
	return document.initialized && document.record.ExclusiveClaims.Kind == "exact"
}

func (document recordedDocument) RequireRetainedNamespace(ctx context.Context, current mutation.DirectoryEntryAuthorityObservation) error {
	if err := document.require(); err != nil {
		return err
	}
	bindings, err := document.record.canonicalBindingIntents(ctx, document.owner)
	if err != nil {
		return err
	}
	for _, intent := range bindings {
		if binding, present := intent.Before().Get(); present {
			expected := recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(binding.PathAuthority())}
			if err := matchRetainedNativeObservation(expected, current); err != nil {
				return fmt.Errorf("retained document binding namespace changed: %w", err)
			}
		}
	}
	if document.HasExactClaims() {
		claims, err := document.record.ExclusiveClaims.canonicalObservedExact(document.owner, document.operationID, document.record.CapturedTarget)
		if err != nil {
			return err
		}
		before := claims.Before()
		if len(before) != 0 {
			expected := recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(before[0].Address().PathAuthority())}
			if err := matchRetainedNativeObservation(expected, current); err != nil {
				return fmt.Errorf("retained document claim namespace changed: %w", err)
			}
		}
	}
	return ctx.Err()
}

func matchRetainedNativeObservation(record recoveryDocumentObservation, current mutation.DirectoryEntryAuthorityObservation) error {
	if err := record.validate(); err != nil {
		return err
	}
	if err := current.Validate(); err != nil {
		return err
	}
	if record.Kind == "exact" {
		expected, err := canonicalPathAuthority(*record.Authority)
		if err != nil {
			return err
		}
		if exact, known := current.Exact(); known && exact.Equal(expected) {
			return nil
		}
		if missing, known := current.Provisional(); known && missing.MatchesMissingExact(expected) {
			return nil
		}
	} else {
		provisional, err := record.provisional()
		if err != nil {
			return err
		}
		if pending, known := current.Provisional(); known && pending.Equal(provisional) {
			return nil
		}
		if exact, known := current.Exact(); known {
			return provisional.AdmitsExact(exact)
		}
	}
	return fmt.Errorf("native document path differs from the retained namespace observation")
}
