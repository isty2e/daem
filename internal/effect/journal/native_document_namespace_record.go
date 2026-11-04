package journal

import (
	"context"
	"fmt"
	"path/filepath"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// This combined read stage is still not an executable operation authority:
// active-journal, state/control, registry, phase and execution-lease joins remain.
type nativeQualifiedDocumentRecords struct {
	records    nativeDocumentRecords
	namespaces []journalnamespace.DocumentNamespace
}

func (journal nativeJournalSnapshots) loadQualifiedDocuments(ctx context.Context, options nativeDocumentReadOptions, roots journalnamespace.Roots) (nativeQualifiedDocumentRecords, error) {
	namespaces, err := journal.qualifyDocumentNamespaces(ctx, options, roots)
	if err != nil {
		return nativeQualifiedDocumentRecords{}, err
	}
	records, err := journal.loadDocuments(ctx, options)
	if err != nil {
		return nativeQualifiedDocumentRecords{}, err
	}
	return nativeQualifiedDocumentRecords{records: records, namespaces: namespaces}, nil
}

func (journal nativeJournalSnapshots) qualifyDocumentNamespaces(ctx context.Context, options nativeDocumentReadOptions, roots journalnamespace.Roots) ([]journalnamespace.DocumentNamespace, error) {
	if ctx == nil || options.filesystem == nil || options.budget == nil {
		return nil, fmt.Errorf("qualified document read context, filesystem and shared budget are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	grants, err := roots.Qualify(ctx, options.budget)
	if err != nil {
		return nil, err
	}
	manifest, present := grants.Manifest()
	if !present || persistedNativeRoot(manifest.Provenance()) != journal.record.ManifestRootProvenance {
		return nil, fmt.Errorf("native journal manifest root does not match the selected live root")
	}
	namespaces := make([]journalnamespace.DocumentNamespace, 0, len(journal.record.DocumentGroups))
	for _, record := range journal.record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		aliases := make([]aggregate.DocumentAddress, 0, len(record.Aliases))
		for _, alias := range record.Aliases {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			address, err := alias.Address.canonical()
			if err != nil {
				return nil, err
			}
			aliases = append(aliases, address)
		}
		provenance, err := record.Writer.Root.canonical()
		if err != nil {
			return nil, err
		}
		relative, err := rootedpath.NewRelativeDestination(record.Writer.RelativePath)
		if err != nil {
			return nil, err
		}
		if relative.Path() != record.Writer.RelativePath {
			return nil, fmt.Errorf("recorded writer relative referent is invalid")
		}
		pin, err := journalnamespace.NewWriterPin(provenance, relative)
		if err != nil {
			return nil, err
		}
		namespace, err := journalnamespace.Reobserve(ctx, aliases, roots, options.filesystem, options.budget, pin)
		if err != nil {
			return nil, err
		}
		if err := matchRetainedNativeObservation(record.CapturedTarget, namespace.Observation()); err != nil {
			return nil, err
		}
		if err := journal.qualifyRetainedDocumentNamespace(ctx, record, namespace.Observation()); err != nil {
			return nil, err
		}
		currentAliases := make(map[aggregate.DocumentAddress]journalnamespace.Alias, len(namespace.Aliases()))
		for _, alias := range namespace.Aliases() {
			currentAliases[alias.Address()] = alias
		}
		for _, alias := range record.Aliases {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			address, err := alias.Address.canonical()
			if err != nil {
				return nil, err
			}
			current, present := currentAliases[address]
			if !present || persistedNativeRoot(current.Root().Provenance()) != alias.LogicalRoot.Provenance {
				return nil, fmt.Errorf("native document alias root incarnation changed")
			}
			if err := matchRetainedNativeObservation(alias.SelectedObservation, current.Observation()); err != nil {
				return nil, err
			}
			if err := matchRetainedNativeObservation(alias.ObservedBefore, current.Observation()); err != nil {
				return nil, err
			}
		}
		if record.PiOrder != nil {
			if err := qualifyNativePiContext(ctx, *record.PiOrder, manifest, namespace, options.budget); err != nil {
				return nil, err
			}
		}
		if record.Removal != nil {
			removal, err := record.Removal.canonical()
			if err != nil {
				return nil, err
			}
			observed, err := namespace.ObserveRemovalNamespace(ctx, removal.Namespace(), options.budget)
			if err != nil {
				return nil, err
			}
			if observed.Status() != recovery.RemovalNamespaceMatched {
				return nil, fmt.Errorf("native document removal namespace no longer matches its writer")
			}
		}
		namespaces = append(namespaces, namespace)
	}
	protected := []pathauthority.Exact{journal.owner.StatefileAuthority()}
	if options.artifacts != nil {
		artifacts, err := options.artifacts.AuthorityBounded(options.budget)
		if err != nil {
			return nil, err
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(artifacts.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, options.budget)
		if err != nil {
			return nil, err
		}
		exact, present := observed.Exact()
		if !present {
			return nil, fmt.Errorf("native recovery artifacts require exact path authority")
		}
		protected = append(protected, exact)
	}
	if err := journal.record.qualifyWriterPartition(ctx, namespaces, manifest, options.ordinaryCapabilities, options.budget, protected); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return namespaces, nil
}

func (journal nativeJournalSnapshots) qualifyRetainedDocumentNamespace(ctx context.Context, record recoveryDocumentGroup, current mutation.DirectoryEntryAuthorityObservation) error {
	bindings, err := record.canonicalBindingIntents(ctx, journal.owner)
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
	if record.ExclusiveClaims.Kind == "exact" {
		claims, err := record.ExclusiveClaims.canonicalObservedExact(journal.owner, journal.record.OperationID, record.CapturedTarget)
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

func qualifyNativePiContext(ctx context.Context, record recoveryDocumentPiOrder, manifest journalnamespace.RootGrant, namespace journalnamespace.DocumentNamespace, budget rootedpath.PhysicalTraversalBudget) error {
	if persistedNativeRoot(manifest.Provenance()) != record.CommandRoot.Provenance {
		return fmt.Errorf("Pi command root differs from the selected manifest incarnation")
	}
	for _, relation := range record.Evidence.Relations {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := manifest.ValidateSelection(relation.SourceContext.BaseRoot, budget); err != nil {
			return err
		}
	}
	settingsPath := filepath.Join(record.Evidence.SettingsContext.BaseRoot, "settings.json")
	referent, err := rootedpath.ResolveReferentPathBounded(settingsPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(referent, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return err
	}
	if !observation.Equal(namespace.Observation()) {
		return fmt.Errorf("captured Pi settings context does not identify the current settings referent")
	}
	return ctx.Err()
}

func nativeDocumentObservationKey(observation mutation.DirectoryEntryAuthorityObservation) string {
	if exact, known := observation.Exact(); known {
		return exact.Key()
	}
	provisional, _ := observation.Provisional()
	return provisional.CandidateKey()
}
