package relationhost

import (
	"fmt"

	observeantigravity "github.com/isty2e/daem/internal/assurance/observe/antigravityplugin"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

func antigravityCLIPluginObserver() passiveObserver {
	return passiveObserver{
		carrier: desiredextension.CarrierAntigravityCLIPlugin,
		observe: observeAntigravityCLIPlugins,
	}
}

func observeAntigravityCLIPlugins(
	_ Input,
	records []carrierRecord,
) (relationobserve.BatchSpec, error) {
	paths, err := observeantigravity.ResolveHostPaths()
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}
	before, err := observeantigravity.ReadInventory(paths)
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}

	spec := relationobserve.BatchSpec{
		Correlations: make([]relationobserve.Correlation, 0, len(records)),
	}
	authorityPaths := make(map[string]struct{})
	for _, record := range records {
		if record.scope != target.ScopeGlobal {
			return relationobserve.BatchSpec{}, fmt.Errorf(
				"Antigravity CLI plugin relation scope %q is not observable",
				record.scope,
			)
		}
		source, err := extensiontopology.InterpretCarrierSource(record.carrierKey)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		if source.Class() != extensiontopology.CarrierSourceMarketplace {
			inventory, err := relationobserve.NewInventory(relationobserve.InventorySpec{
				Availability: relationobserve.InventoryUnsupported,
				Freshness:    relationobserve.EvidenceFresh,
			})
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			spec.Correlations = append(spec.Correlations, relationobserve.Correlation{
				Key: record.key,
				Result: relationobserve.Correlate(
					record.key.ExpectedRelation(),
					inventory,
				),
			})
			continue
		}
		var result relationobserve.CorrelationResult
		if record.desiredPresent {
			result, err = before.CorrelateDesired(
				record.key,
				record.carrierKey,
			)
		} else {
			result, err = before.CorrelateRemoval(
				record.key,
				record.carrierKey,
			)
		}
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		spec.Correlations = append(spec.Correlations, relationobserve.Correlation{
			Key:    record.key,
			Result: result,
		})
		pluginManifestPath, err := paths.PluginManifestPath(source.RelationIdentity())
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		for _, path := range []string{
			paths.ImportManifestPath(),
			pluginManifestPath,
		} {
			if _, exists := authorityPaths[path]; exists {
				continue
			}
			authority, err := relationobserve.NewAuthorityPath(
				path,
				target.TargetAntigravityCLI,
				target.ScopeGlobal,
			)
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			spec.AuthorityPaths = append(spec.AuthorityPaths, authority)
			authorityPaths[path] = struct{}{}
		}
	}
	after, err := observeantigravity.ReadInventory(paths)
	if err != nil {
		return relationobserve.BatchSpec{}, err
	}
	if !before.Equal(after) {
		return relationobserve.BatchSpec{}, fmt.Errorf(
			"Antigravity CLI plugin import manifest changed during observation",
		)
	}
	return spec, nil
}
