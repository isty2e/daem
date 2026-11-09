package qualification

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

func LoadNamespaces(journal journalrecord.Native, ctx context.Context, options ReadOptions, roots journalnamespace.Roots) ([]journalnamespace.DocumentNamespace, error) {
	if ctx == nil || options.Filesystem == nil || options.Budget == nil {
		return nil, fmt.Errorf("qualified document read context, filesystem and shared budget are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	documents, err := journal.Documents()
	if err != nil {
		return nil, err
	}
	expected, err := options.Rules.index()
	if err != nil {
		return nil, err
	}
	grants, err := roots.Qualify(ctx, options.Budget)
	if err != nil {
		return nil, err
	}
	manifest, present := grants.Manifest()
	manifestReference, err := journal.ManifestRootReference()
	if err != nil {
		return nil, err
	}
	if !present || manifest.RootReference() != manifestReference || !journal.MatchesManifestRoot(manifest.Provenance()) {
		return nil, fmt.Errorf("native journal manifest root does not match the selected live root")
	}
	namespaces := make([]journalnamespace.DocumentNamespace, 0, len(documents))
	for _, record := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		recordedAliases := record.Aliases()
		aliases := make([]journalnamespace.RecordedAlias, 0, len(recordedAliases))
		for _, alias := range recordedAliases {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			address, err := alias.Address()
			if err != nil {
				return nil, err
			}
			location, err := alias.Location()
			if err != nil {
				return nil, err
			}
			aliases = append(aliases, journalnamespace.RecordedAlias{Address: address, Location: location})
		}
		pin, err := record.WriterPin()
		if err != nil {
			return nil, err
		}
		namespace, err := journalnamespace.Reobserve(ctx, aliases, roots, options.Protection, options.Filesystem, options.Budget, pin)
		if err != nil {
			return nil, err
		}
		if options.Rules.mode == forwardNamespaceRules {
			original, present := expected[aliases[0].Address]
			if !present {
				return nil, fmt.Errorf("native forward document has no original rule observation")
			}
			if err := namespace.RequirePhysicalRules(original); err != nil {
				return nil, err
			}
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
		for _, alias := range recordedAliases {
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
			if err := QualifyPiContext(ctx, pi, manifest, namespace, options.Budget); err != nil {
				return nil, err
			}
		}
		removal, present, err := record.Removal()
		if err != nil {
			return nil, err
		}
		if present {
			observed, err := namespace.ObserveRemovalNamespace(ctx, removal.Namespace(), options.Budget)
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
	if options.Artifacts != nil {
		artifacts, err := options.Artifacts.AuthorityBounded(options.Budget)
		if err != nil {
			return nil, err
		}
		observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(artifacts.PhysicalRoot(), physicalwork.MaximumPhysicalPathDepth, options.Budget)
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
	if err := QualifyWriterPartition(ctx, entries, namespaces, manifest, options.OrdinaryCapabilities, options.Budget, protected); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return namespaces, nil
}

func QualifyPiContext(ctx context.Context, facts journalrecord.PiQualification, manifest journalnamespace.RootGrant, namespace journalnamespace.DocumentNamespace, budget rootedpath.PhysicalTraversalBudget) error {
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
