package relationhost

import (
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func claudePluginObserver() passiveObserver {
	return passiveObserver{
		carrier: desiredextension.CarrierClaudeCodePlugin,
		observe: observeClaudePlugins,
	}
}

func observeClaudePlugins(input Input, records []carrierRecord) (relationobserve.BatchSpec, error) {
	relations := make([]observeclaudeplugin.ScopedRelation, 0, len(records))
	for _, record := range records {
		scoped, err := observeclaudeplugin.NewScopedRelation(record.key, record.scope)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		relations = append(relations, scoped)
	}

	inventoryInput := observeclaudeplugin.InstalledInventoryInput{
		WorkDir:     input.Paths.ManifestRoot,
		ProjectRoot: input.Paths.ManifestRoot,
		Relations:   relations,
	}
	inventory, err := observeclaudeplugin.ReadInstalledInventory(inventoryInput)
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}
	inventoryPath, err := observeclaudeplugin.InstalledInventoryPath(inventoryInput)
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}
	authorityPath, err := relationobserve.NewAuthorityPath(inventoryPath, target.TargetClaudeCode, target.ScopeGlobal)
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}

	correlations := make([]relationobserve.Correlation, 0, len(records))
	for index, record := range records {
		correlations = append(correlations, relationobserve.Correlation{
			Key:    record.key,
			Result: observeclaudeplugin.Correlate(relations[index], inventory),
		})
	}
	return relationobserve.BatchSpec{
		Correlations:   correlations,
		AuthorityPaths: []relationobserve.AuthorityPath{authorityPath},
	}, nil
}
