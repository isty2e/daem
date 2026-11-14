package configuration

import (
	"slices"

	"github.com/isty2e/daem/internal/output"
)

// ProtectionLocation describes selector coverage, not a writable output root.
// Optional locations may explicitly contribute no additional filesystem path.
type ProtectionLocation struct {
	reference output.RootReference
	optional  bool
}

func (location ProtectionLocation) Reference() output.RootReference { return location.reference }
func (location ProtectionLocation) Optional() bool                  { return location.optional }

func NativeProtectionLocations() ([]ProtectionLocation, error) {
	contracts, err := NativeRootContracts()
	if err != nil {
		return nil, err
	}
	extra, err := openCodeExtra.reference()
	if err != nil {
		return nil, err
	}
	sources, err := openCodeSources.reference()
	if err != nil {
		return nil, err
	}
	result := make([]ProtectionLocation, 0, len(contracts)+6)
	project, err := ProjectRootReference()
	if err != nil {
		return nil, err
	}
	result = append(result, ProtectionLocation{reference: project})
	for _, contract := range contracts {
		result = append(result, ProtectionLocation{reference: contract.Reference(), optional: contract.Reference() == extra || contract.Reference() == sources})
	}
	for _, entry := range []struct {
		root     rootFact
		optional bool
	}{
		{openCodeData, false},
		{openCodeState, false},
		{openCodeCache, false},
		{openCodeDatabase, true},
		{openCodeMetadata, true},
		{openCodeInstall, true},
		{claudeCredentials, true},
		{claudePlugins, true},
		{antigravityState, false},
	} {
		reference, err := entry.root.reference()
		if err != nil {
			return nil, err
		}
		result = append(result, ProtectionLocation{reference: reference, optional: entry.optional})
	}
	slices.SortFunc(result, func(left, right ProtectionLocation) int {
		return left.reference.Compare(right.reference)
	})
	return result, nil
}
