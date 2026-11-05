package journal

import (
	"context"
	"fmt"
	"path/filepath"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"

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
	records    journalrecord.LoadedDocuments
	namespaces []journalnamespace.DocumentNamespace
}

func loadQualifiedDocuments(journal journalrecord.Native, ctx context.Context, options nativeDocumentReadOptions, roots journalnamespace.Roots) (nativeQualifiedDocumentRecords, error) {
	namespaces, err := qualifyDocumentNamespaces(journal, ctx, options, roots)
	if err != nil {
		return nativeQualifiedDocumentRecords{}, err
	}
	records, err := loadDocuments(journal, ctx, options)
	if err != nil {
		return nativeQualifiedDocumentRecords{}, err
	}
	return nativeQualifiedDocumentRecords{records: records, namespaces: namespaces}, nil
}

func qualifyDocumentNamespaces(journal journalrecord.Native, ctx context.Context, options nativeDocumentReadOptions, roots journalnamespace.Roots) ([]journalnamespace.DocumentNamespace, error) {
	if ctx == nil || options.filesystem == nil || options.budget == nil {
		return nil, fmt.Errorf("qualified document read context, filesystem and shared budget are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	documents, err := journal.Documents()
	if err != nil {
		return nil, err
	}
	grants, err := roots.Qualify(ctx, options.budget)
	if err != nil {
		return nil, err
	}
	manifest, present := grants.Manifest()
	if !present || !journal.MatchesManifestRoot(manifest.Provenance()) {
		return nil, fmt.Errorf("native journal manifest root does not match the selected live root")
	}
	namespaces := make([]journalnamespace.DocumentNamespace, 0, len(documents))
	for _, record := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		aliases, err := record.Addresses(ctx)
		if err != nil {
			return nil, err
		}
		provenance, relative, err := record.Writer()
		if err != nil {
			return nil, err
		}
		pin, err := journalnamespace.NewWriterPin(provenance, relative)
		if err != nil {
			return nil, err
		}
		namespace, err := journalnamespace.Reobserve(ctx, aliases, roots, options.filesystem, options.budget, pin)
		if err != nil {
			return nil, err
		}
		if err := record.RequireCapturedNamespace(namespace.Observation()); err != nil {
			return nil, err
		}
		if err := record.RequireRetainedNamespace(ctx, namespace.Observation()); err != nil {
			return nil, err
		}
		currentAliases := make(map[aggregate.DocumentAddress]journalnamespace.Alias, len(namespace.Aliases()))
		for _, alias := range namespace.Aliases() {
			currentAliases[alias.Address()] = alias
		}
		for _, alias := range record.Aliases() {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			address, err := alias.Address()
			if err != nil {
				return nil, err
			}
			current, present := currentAliases[address]
			if !present || !alias.MatchesRoot(current.Root().Provenance()) {
				return nil, fmt.Errorf("native document alias root incarnation changed")
			}
			if err := alias.RequireObservations(current.Observation()); err != nil {
				return nil, err
			}
		}
		pi, present, err := record.PiQualification()
		if err != nil {
			return nil, err
		}
		if present {
			if err := qualifyNativePiContext(ctx, pi, manifest, namespace, options.budget); err != nil {
				return nil, err
			}
		}
		removal, present, err := record.Removal()
		if err != nil {
			return nil, err
		}
		if present {
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
	protected := []pathauthority.Exact{journal.Owner().StatefileAuthority()}
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
	entries, err := journal.RecordedEntries()
	if err != nil {
		return nil, err
	}
	if err := qualifyWriterPartition(ctx, entries, namespaces, manifest, options.ordinaryCapabilities, options.budget, protected); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return namespaces, nil
}

func qualifyNativePiContext(ctx context.Context, facts journalrecord.PiQualification, manifest journalnamespace.RootGrant, namespace journalnamespace.DocumentNamespace, budget rootedpath.PhysicalTraversalBudget) error {
	if manifest.Provenance() != facts.CommandRoot() {
		return fmt.Errorf("Pi command root differs from the selected manifest incarnation")
	}
	for _, root := range facts.RelationBaseRoots() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := manifest.ValidateSelection(root, budget); err != nil {
			return err
		}
	}
	settingsPath := filepath.Join(facts.SettingsBaseRoot(), "settings.json")
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
