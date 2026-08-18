package codexplugin

import (
	"fmt"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

// CorrelateConfig classifies one exact Codex config relation. Unsupported
// unrelated rows are ignored because they cannot affect the selected key.
func CorrelateConfig(
	key observerelation.CorrelationKey,
	observation observeconfig.Observation,
) (observerelation.CorrelationResult, error) {
	if err := key.Validate(); err != nil {
		return observerelation.CorrelationResult{}, fmt.Errorf("Codex relation correlation key: %w", err)
	}
	expected := key.ExpectedRelation()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		string(expected.SubjectKey()),
	)
	if err != nil {
		return observerelation.CorrelationResult{}, fmt.Errorf("Codex relation source: %w", err)
	}
	if _, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	); err != nil {
		return observerelation.CorrelationResult{}, fmt.Errorf("Codex relation carrier: %w", err)
	}
	if observation.EntrySetUnsupported() {
		return observerelation.CorrelationResult{}, fmt.Errorf("Codex plugins config container is unsupported")
	}
	if observation.EntrySetBudgetExceeded() {
		return observerelation.CorrelationResult{}, fmt.Errorf("Codex plugins config exceeds observation budget")
	}

	rows := make([]observerelation.Row, 0, 1)
	if observation.EntrySetObserved() {
		for _, entry := range observation.Entries() {
			if string(entry.Key()) != source.Ref() {
				continue
			}
			if !entry.Observed() {
				return observerelation.CorrelationResult{}, fmt.Errorf(
					"Codex plugin config entry %q is unsupported: %s",
					entry.Key(),
					entry.Reason(),
				)
			}
			row, err := observerelation.NewRow(observerelation.RowSpec{
				SubjectKey:            source.Ref(),
				HasManagedInstanceKey: true,
				ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
			})
			if err != nil {
				return observerelation.CorrelationResult{}, err
			}
			rows = append(rows, row)
		}
	}
	inventory, err := observerelation.NewInventory(observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
		Rows:         rows,
	})
	if err != nil {
		return observerelation.CorrelationResult{}, err
	}
	return observerelation.Correlate(expected, inventory), nil
}
