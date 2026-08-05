package extension

import (
	"fmt"
	"path/filepath"
	"strings"

	observeantigravity "github.com/isty2e/daem/internal/assurance/observe/antigravityplugin"
	observeclaude "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observecodex "github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	observepi "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/realization/profile"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func (collector *importCollector) collectClaude() error {
	inventoryInput := observeclaude.InstalledInventoryInput{
		WorkDir:     collector.input.ManifestRoot,
		ProjectRoot: collector.input.ManifestRoot,
	}
	inventoryPath, err := observeclaude.InstalledInventoryPath(inventoryInput)
	if err != nil {
		return err
	}
	inventory, err := observeclaude.ReadInstalledInventory(inventoryInput)
	if err != nil {
		return err
	}
	for _, scope := range collector.input.Scopes {
		sources, err := inventory.ExactSources(scope)
		if err != nil {
			return err
		}
		for _, source := range sources {
			if err := collector.addSource(
				desiredextension.CarrierClaudeCodePlugin,
				target.TargetClaudeCode,
				scope,
				desiredextension.SourceKindMarketplace,
				source,
				source,
			); err != nil {
				return err
			}
		}
		collector.scans = append(collector.scans, Scan{
			LivePath: inventoryPath,
			Target:   target.TargetClaudeCode,
			Scope:    scope,
			Entries:  len(sources),
			Imported: len(sources),
		})
	}
	return nil
}

func (collector *importCollector) collectCodex() error {
	paths, err := observecodex.ResolveHostPaths()
	if err != nil {
		return err
	}
	observation, err := observecodex.ObserveConfigFile(paths.ConfigPath())
	if err != nil {
		return err
	}
	sources, err := observecodex.ExactConfiguredSources(observation)
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := collector.addSource(
			desiredextension.CarrierCodexPlugin,
			target.TargetCodex,
			target.ScopeGlobal,
			desiredextension.SourceKindMarketplace,
			source,
			source,
		); err != nil {
			return err
		}
	}
	collector.scans = append(collector.scans, Scan{
		LivePath: paths.ConfigPath(),
		Target:   target.TargetCodex,
		Scope:    target.ScopeGlobal,
		Entries:  len(sources),
		Imported: len(sources),
	})
	return nil
}

func (collector *importCollector) collectOpenCode(scope target.Scope) error {
	inventory, err := observeopencode.ReadInventory(
		observeopencode.InventoryInput{
			ManifestRoot: collector.input.ManifestRoot,
			Scope:        scope,
		},
	)
	if err != nil {
		return err
	}
	collector.openCodeSourcePaths[scope] = filepath.Join(
		inventory.Directory(),
		"opencode.json",
	)
	capability, admitted := profile.Profile(target.TargetOpenCode).ExtensionOrder(
		desiredextension.CarrierOpenCodePlugin,
		scope,
	)
	if !admitted {
		return fmt.Errorf("OpenCode %s extension order capability is absent", scope)
	}

	for _, document := range inventory.Documents() {
		importedCount := 0
		skippedCount := 0
		rows := make([]sequenceRowFact, 0, len(document.Entries()))
		for index, entry := range document.Entries() {
			loadIdentity, err := hostrelation.NewHostLoadIdentity(
				entry.HostLoadIdentity(),
			)
			if err != nil {
				return err
			}
			row := sequenceRowFact{loadIdentity: loadIdentity}
			key, err := newCarrierKey(
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				scope,
				desiredextension.SourceKindHostSource,
				entry.Source(),
			)
			if err != nil {
				skippedCount++
				collector.skipped = append(collector.skipped, Skip{
					LivePath: fmt.Sprintf("%s#plugin[%d]", document.Path(), index),
					Reason:   reasonSourceNotImportable,
				})
			} else {
				if err := collector.addCandidate(key, loadIdentity); err != nil {
					return err
				}
				row.key = key
				row.correlated = true
				importedCount++
			}
			rows = append(rows, row)
		}
		sequenceID, err := opencodeconfig.PhysicalSequenceID(
			scope,
			document.Kind(),
			document.Path(),
		)
		if err != nil {
			return err
		}
		if !capability.AdmitsPhysicalSequenceID(sequenceID) {
			return fmt.Errorf(
				"OpenCode extension order class %q does not admit physical sequence %q",
				capability.ClassID(),
				sequenceID,
			)
		}
		authority, err := relationobserve.NewSequenceAuthority(
			"opencode:" + string(scope) + ":" + string(document.Kind()) +
				"." + strings.TrimPrefix(filepath.Ext(document.Path()), "."),
		)
		if err != nil {
			return err
		}
		revision, err := relationobserve.NewSequenceRevision(document.Revision())
		if err != nil {
			return err
		}
		collector.sequences = append(collector.sequences, sequenceFact{
			classID:   capability.ClassID(),
			sequence:  sequenceID,
			authority: authority,
			revision:  revision,
			rows:      rows,
		})
		collector.scans = append(collector.scans, Scan{
			LivePath: document.Path(),
			Target:   target.TargetOpenCode,
			Scope:    scope,
			Entries:  len(document.Entries()),
			Imported: importedCount,
			Skipped:  skippedCount,
		})
	}
	return nil
}

func (collector *importCollector) collectPi(scope target.Scope) error {
	inventory, err := observepi.ReadSettings(observepi.SettingsInput{
		WorkDir:     collector.input.ManifestRoot,
		ProjectRoot: collector.input.ManifestRoot,
		Scope:       scope,
	})
	if err != nil {
		return err
	}
	entries, err := inventory.Entries()
	if err != nil {
		return err
	}
	capability, admitted := profile.Profile(target.TargetPi).ExtensionOrder(
		desiredextension.CarrierPiPackage,
		scope,
	)
	if !admitted {
		return fmt.Errorf("Pi %s extension order capability is absent", scope)
	}

	rows := make([]sequenceRowFact, 0, len(entries))
	for _, entry := range entries {
		source := entry.Source()
		if localPath, local := entry.LocalIdentity(); local {
			identity, err := extensiontopology.NewLocalSourceIdentity(localPath)
			if err != nil {
				return err
			}
			source, err = declarationmanifest.LocalSourceManifestReference(
				identity,
				collector.input.ManifestRoot,
				scope,
			)
			if err != nil {
				return err
			}
		}
		key, err := newCarrierKey(
			desiredextension.CarrierPiPackage,
			target.TargetPi,
			scope,
			desiredextension.SourceKindHostSource,
			source,
		)
		if err != nil {
			return err
		}
		loadIdentity, err := hostrelation.NewHostLoadIdentity(
			entry.HostLoadIdentity(),
		)
		if err != nil {
			return err
		}
		if err := collector.addCandidate(key, loadIdentity); err != nil {
			return err
		}
		rows = append(rows, sequenceRowFact{
			loadIdentity: loadIdentity,
			key:          key,
			correlated:   true,
		})
	}

	sequenceIDs := capability.PhysicalSequenceIDs()
	if len(sequenceIDs) != 1 {
		return fmt.Errorf(
			"Pi %s extension order class requires one physical sequence",
			scope,
		)
	}
	authority, err := relationobserve.NewSequenceAuthority(
		"pi:" + string(scope) + ":settings.packages",
	)
	if err != nil {
		return err
	}
	revision, err := relationobserve.NewSequenceRevision(inventory.Revision())
	if err != nil {
		return err
	}
	collector.sequences = append(collector.sequences, sequenceFact{
		classID:   capability.ClassID(),
		sequence:  sequenceIDs[0],
		authority: authority,
		revision:  revision,
		rows:      rows,
	})
	collector.scans = append(collector.scans, Scan{
		LivePath: inventory.SettingsPath(),
		Target:   target.TargetPi,
		Scope:    scope,
		Entries:  len(entries),
		Imported: len(entries),
	})
	return nil
}

func (collector *importCollector) collectAntigravity() error {
	paths, err := observeantigravity.ResolveHostPaths()
	if err != nil {
		return err
	}
	inventory, err := observeantigravity.ReadInventory(paths)
	if err != nil {
		return err
	}
	names, err := inventory.CompletePluginNames()
	if err != nil {
		return err
	}
	for _, name := range names {
		collector.skipped = append(collector.skipped, Skip{
			LivePath: paths.ImportManifestPath() + "#plugin=" + name,
			Reason:   reasonSourceProvenanceUnrecoverable,
		})
	}
	collector.scans = append(collector.scans, Scan{
		LivePath: paths.ImportManifestPath(),
		Target:   target.TargetAntigravityCLI,
		Scope:    target.ScopeGlobal,
		Entries:  len(names),
		Skipped:  len(names),
	})
	return nil
}
