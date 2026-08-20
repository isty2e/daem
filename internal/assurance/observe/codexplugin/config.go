package codexplugin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/target"
)

// MaximumConfigBytes bounds one Codex plugin configuration observation.
const MaximumConfigBytes = 4 << 20

func ObserveConfigFile(configPath string) (observeconfig.Observation, error) {
	return observeConfigFile(context.Background(), configPath, &observationBudget{})
}

func observeConfigFile(
	ctx context.Context,
	configPath string,
	budget *observationBudget,
) (observeconfig.Observation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if budget == nil {
		budget = &observationBudget{}
	}
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return observeconfig.Observation{}, fmt.Errorf("Codex config path is required")
	}

	observation, err := configObservation(configPath, false, observeconfig.EntrySetNotDeclared, nil)
	if err != nil {
		return observeconfig.Observation{}, err
	}
	result, err := filesnapshot.ReadRegularFileContextCounted(ctx, configPath, MaximumConfigBytes)
	_ = chargeSnapshotAttempt(budget, result.Attempted, result.Exists, err)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return observation, nil
		}
		return observation, err
	}
	if !result.Exists {
		return observation, nil
	}
	existingObservation, err := configObservation(configPath, true, observeconfig.EntrySetNotDeclared, nil)
	if err != nil {
		return observeconfig.Observation{}, err
	}

	if err := tomlstrict.Admit(ctx, result.Content, tomlstrict.StandardLimits()); err != nil {
		return existingObservation, err
	}

	var decoded map[string]any
	if _, err := toml.Decode(string(result.Content), &decoded); err != nil {
		return existingObservation, err
	}

	pluginsValue, ok := decoded["plugins"]
	if !ok {
		return existingObservation, nil
	}
	plugins, ok := pluginsValue.(map[string]any)
	if !ok {
		return configObservation(configPath, true, observeconfig.EntrySetUnsupported, nil)
	}

	for key := range plugins {
		if budget.consumeNames([]string{key}) {
			return configObservation(configPath, true, observeconfig.EntrySetBudgetExceeded, nil)
		}
	}
	keys := sortedMapKeys(plugins)
	entries := make([]observeconfig.Entry, 0, len(keys))
	for _, key := range keys {
		entry, err := pluginEntryObservation(key, plugins[key])
		if err != nil {
			return existingObservation, err
		}
		entries = append(entries, entry)
	}
	return configObservation(configPath, true, observeconfig.EntrySetObserved, entries)
}

// ExactConfiguredSources returns every supported configured Codex plugin
// selector. Unsupported selected rows fail closed instead of becoming
// approximate import candidates.
func ExactConfiguredSources(observation observeconfig.Observation) ([]string, error) {
	if observation.EntrySetUnsupported() {
		return nil, fmt.Errorf("Codex plugins config container is unsupported")
	}
	if observation.EntrySetBudgetExceeded() {
		return nil, fmt.Errorf("Codex plugins config exceeds observation budget")
	}
	if !observation.EntrySetObserved() {
		return nil, nil
	}

	entries := observation.Entries()
	sources := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.Observed() {
			return nil, fmt.Errorf(
				"Codex plugin config entry %q is unsupported: %s",
				entry.Key(),
				entry.Reason(),
			)
		}
		source, err := desiredextension.NewSourceRef(
			desiredextension.SourceKindMarketplace,
			string(entry.Key()),
		)
		if err != nil {
			return nil, fmt.Errorf("Codex plugin config entry %q: %w", entry.Key(), err)
		}
		if _, err := desiredextension.NewCarrierKey(
			desiredextension.CarrierCodexPlugin,
			target.TargetCodex,
			target.ScopeGlobal,
			source,
		); err != nil {
			return nil, fmt.Errorf("Codex plugin config entry %q: %w", entry.Key(), err)
		}
		sources = append(sources, source.Ref())
	}
	sort.Strings(sources)
	return sources, nil
}

func pluginEntryObservation(configKey string, rawValue any) (observeconfig.Entry, error) {
	if strings.TrimSpace(configKey) == "" {
		return observeconfig.NewEntry(observeconfig.EntrySpec{
			Key:        observeconfig.Key(configKey),
			State:      observeconfig.EntryUnsupported,
			Activation: observeconfig.ActivationNotDeclared,
			Reason:     observeconfig.ReasonEmptyEntryKey,
		})
	}

	table, ok := rawValue.(map[string]any)
	if !ok {
		return observeconfig.NewEntry(observeconfig.EntrySpec{
			Key:        observeconfig.Key(configKey),
			State:      observeconfig.EntryUnsupported,
			Activation: observeconfig.ActivationNotDeclared,
			Reason:     observeconfig.ReasonEntryNotTable,
		})
	}

	enabledValue, ok := table["enabled"]
	if !ok {
		return observeconfig.NewEntry(observeconfig.EntrySpec{
			Key:        observeconfig.Key(configKey),
			State:      observeconfig.EntryObserved,
			Activation: observeconfig.ActivationNotDeclared,
		})
	}
	enabled, ok := enabledValue.(bool)
	if !ok {
		return observeconfig.NewEntry(observeconfig.EntrySpec{
			Key:        observeconfig.Key(configKey),
			State:      observeconfig.EntryUnsupported,
			Activation: observeconfig.ActivationUnsupportedType,
			Reason:     observeconfig.ReasonActivationNotBoolean,
		})
	}
	if enabled {
		return observeconfig.NewEntry(observeconfig.EntrySpec{
			Key:        observeconfig.Key(configKey),
			State:      observeconfig.EntryObserved,
			Activation: observeconfig.ActivationConfiguredTrue,
		})
	}
	return observeconfig.NewEntry(observeconfig.EntrySpec{
		Key:        observeconfig.Key(configKey),
		State:      observeconfig.EntryObserved,
		Activation: observeconfig.ActivationConfiguredFalse,
	})
}

func configObservation(
	configPath string,
	configExists bool,
	entrySetState observeconfig.EntrySetState,
	entries []observeconfig.Entry,
) (observeconfig.Observation, error) {
	return observeconfig.NewObservation(observeconfig.ObservationSpec{
		SourcePath:    configPath,
		ConfigExists:  configExists,
		EntrySetState: entrySetState,
		Entries:       entries,
	})
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
