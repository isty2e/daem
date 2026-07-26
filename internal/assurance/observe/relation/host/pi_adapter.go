package relationhost

import (
	"fmt"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
)

func piPackageObserver() passiveObserver {
	return passiveObserver{
		carrier: desiredextension.CarrierPiPackage,
		observe: observePiPackages,
	}
}

func observePiPackages(input Input, records []carrierRecord) (relationobserve.BatchSpec, error) {
	recordsByScope := map[target.Scope][]carrierRecord{
		target.ScopeProject: nil,
		target.ScopeGlobal:  nil,
	}
	for _, record := range records {
		recordsByScope[record.scope] = append(recordsByScope[record.scope], record)
	}

	spec := relationobserve.BatchSpec{}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		scopedRecords := recordsByScope[scope]
		if len(scopedRecords) == 0 {
			continue
		}
		inventory, err := observepipackage.ReadSettings(observepipackage.SettingsInput{
			WorkDir:     input.Paths.ManifestRoot,
			ProjectRoot: input.Paths.ManifestRoot,
			Scope:       scope,
		})
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		authorityPath, err := relationobserve.NewAuthorityPath(
			inventory.SettingsPath(),
			target.TargetPi,
			scope,
		)
		if err != nil {
			return relationobserve.BatchSpec{}, err
		}
		spec.AuthorityPaths = append(spec.AuthorityPaths, authorityPath)

		for _, record := range scopedRecords {
			expected, err := observepipackage.NewScopedRelation(
				record.key,
				record.scope,
				input.Paths.ManifestRoot,
			)
			if err != nil {
				return relationobserve.BatchSpec{}, err
			}
			result, err := observepipackage.Correlate(expected, inventory)
			if err != nil {
				return relationobserve.BatchSpec{}, fmt.Errorf(
					"correlate Pi %s package relation: %w",
					scope,
					err,
				)
			}
			spec.Correlations = append(spec.Correlations, relationobserve.Correlation{
				Key:    record.key,
				Result: result,
			})
		}
	}
	return spec, nil
}
