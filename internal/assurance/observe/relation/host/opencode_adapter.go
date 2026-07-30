package relationhost

import (
	"fmt"

	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func openCodePluginObserver() passiveObserver {
	return passiveObserver{
		carrier: desiredextension.CarrierOpenCodePlugin,
		observe: observeOpenCodePlugins,
	}
}

func observeOpenCodePlugins(input Input, records []carrierRecord) (relationobserve.BatchSpec, error) {
	recordsByScope := map[target.Scope][]carrierRecord{
		target.ScopeProject: nil,
		target.ScopeGlobal:  nil,
	}
	for _, record := range records {
		switch record.scope {
		case target.ScopeProject, target.ScopeGlobal:
			recordsByScope[record.scope] = append(recordsByScope[record.scope], record)
		default:
			return relationobserve.BatchSpec{}, fmt.Errorf(
				"OpenCode plugin relation scope %q is not observable",
				record.scope,
			)
		}
	}

	spec := relationobserve.BatchSpec{}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		scopedRecords := recordsByScope[scope]
		if len(scopedRecords) == 0 {
			continue
		}
		inventory, err := observeopencode.ReadInventory(observeopencode.InventoryInput{
			ManifestRoot: input.Paths.ManifestRoot,
			Scope:        scope,
		})
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		documents := inventory.Documents()
		authorityPaths, err := observeopencode.OrderAuthorityPaths(
			observeopencode.InventoryInput{
				ManifestRoot: input.Paths.ManifestRoot,
				Scope:        scope,
			},
		)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		for _, path := range authorityPaths {
			authorityPath, err := relationobserve.NewAuthorityPath(
				path,
				target.TargetOpenCode,
				scope,
			)
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			spec.AuthorityPaths = append(spec.AuthorityPaths, authorityPath)
		}
		for _, record := range scopedRecords {
			result, err := correlateOpenCodeRelation(record, documents)
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			spec.Correlations = append(spec.Correlations, relationobserve.Correlation{
				Key:    record.key,
				Result: result,
			})
		}
	}
	return spec, nil
}

func correlateOpenCodeRelation(
	record carrierRecord,
	documents []observeopencode.Document,
) (relationobserve.CorrelationResult, error) {
	if err := record.key.Validate(); err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf(
			"OpenCode relation correlation key: %w",
			err,
		)
	}
	expected := record.key.ExpectedRelation()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		string(expected.SubjectKey()),
	)
	if err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf("OpenCode relation source: %w", err)
	}
	if _, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierOpenCodePlugin,
		target.TargetOpenCode,
		record.scope,
		source,
	); err != nil {
		return relationobserve.CorrelationResult{}, fmt.Errorf("OpenCode relation carrier: %w", err)
	}

	present := false
	for _, document := range documents {
		if !document.Exists() {
			continue
		}
		switch count := document.ExactSourceCount(source.Ref()); count {
		case 0:
		case 1:
			present = true
		default:
			return relationobserve.CorrelationResult{}, fmt.Errorf(
				"OpenCode config %q contains %d exact plugin rows for source %q",
				document.Path(),
				count,
				source.Ref(),
			)
		}
	}

	rows := make([]relationobserve.Row, 0, 1)
	if present {
		row, err := relationobserve.NewRow(relationobserve.RowSpec{
			SubjectKey:            source.Ref(),
			HasManagedInstanceKey: true,
			ManagedInstanceKey:    string(expected.ManagedInstanceKey()),
		})
		if err != nil {
			return relationobserve.CorrelationResult{}, err
		}
		rows = append(rows, row)
	}
	inventory, err := relationobserve.NewInventory(relationobserve.InventorySpec{
		Availability: relationobserve.InventorySupported,
		Freshness:    relationobserve.EvidenceFresh,
		Rows:         rows,
	})
	if err != nil {
		return relationobserve.CorrelationResult{}, err
	}
	return relationobserve.Correlate(expected, inventory), nil
}
