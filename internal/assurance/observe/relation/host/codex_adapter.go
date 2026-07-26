package relationhost

import (
	"fmt"

	observecodexplugin "github.com/isty2e/daem/internal/assurance/observe/codexplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func codexPluginObserver() passiveObserver {
	return passiveObserver{
		carrier: desiredextension.CarrierCodexPlugin,
		observe: observeCodexPlugins,
	}
}

func observeCodexPlugins(_ Input, records []carrierRecord) (relationobserve.BatchSpec, error) {
	paths, err := observecodexplugin.ResolveHostPaths()
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}
	observation, err := observecodexplugin.ObserveConfigFile(paths.ConfigPath())
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}
	authorityPath, err := relationobserve.NewAuthorityPath(
		paths.ConfigPath(),
		target.TargetCodex,
		target.ScopeGlobal,
	)
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}

	spec := relationobserve.BatchSpec{
		Correlations:   make([]relationobserve.Correlation, 0, len(records)),
		AuthorityPaths: []relationobserve.AuthorityPath{authorityPath},
	}
	for _, record := range records {
		if record.scope != target.ScopeGlobal {
			return relationobserve.BatchSpec{}, fmt.Errorf(
				"Codex plugin relation scope %q is not observable",
				record.scope,
			)
		}
		result, err := observecodexplugin.CorrelateConfig(
			record.key,
			observation,
		)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		spec.Correlations = append(spec.Correlations, relationobserve.Correlation{
			Key:    record.key,
			Result: result,
		})
	}
	return spec, nil
}
