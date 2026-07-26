package statefile

import (
	"fmt"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func canonicalEffectBaselines(
	persisted []effectBaselineDTO,
) (durablecarrier.EffectBaselineSet, error) {
	baselines := make([]durablecarrier.EffectBaseline, 0, len(persisted))
	for index, row := range persisted {
		requirement := effectpostcondition.Requirement(row.Requirement)
		var (
			baseline durablecarrier.EffectBaseline
			err      error
		)
		switch durablecarrier.EffectBaselineState(row.State) {
		case durablecarrier.EffectBaselineAbsent:
			if row.ContentHash != "" {
				err = fmt.Errorf("absent baseline must not carry content")
				break
			}
			baseline, err = durablecarrier.NewAbsentEffectBaseline(requirement)
		case durablecarrier.EffectBaselineContent:
			baseline, err = durablecarrier.NewContentEffectBaseline(
				requirement,
				artifact.ContentHash(row.ContentHash),
			)
		default:
			err = fmt.Errorf("state %q is unsupported", row.State)
		}
		if err != nil {
			return durablecarrier.EffectBaselineSet{}, fmt.Errorf("baseline[%d]: %w", index, err)
		}
		baselines = append(baselines, baseline)
	}
	return durablecarrier.NewEffectBaselineSet(baselines)
}
