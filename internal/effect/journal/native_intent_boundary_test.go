package journal

import (
	"errors"
	"testing"

	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLegacyRecoveryObservationRefusesNativeScopeWithoutPartialResults(t *testing.T) {
	fixture := documentClaimsForTest(t)
	legacy, err := ownership.NewProvisionalAcquireIntentFromLegacy(fixture.alias.AggregateRoot(), "", fixture.provisional, fixture.owner, "legacy-observation")
	if err != nil {
		t.Fatal(err)
	}
	for _, segments := range [][]string{{""}, {"a/b"}, {"."}, {"line\n\x00key"}} {
		native, err := ownership.NewProvisionalAcquireIntent(legacy.Destination(), outputtest.KeyFootprint(t, segments...), legacy.Path(), legacy.Owner(), legacy.OperationID())
		if err != nil {
			t.Fatal(err)
		}
		for _, intents := range [][]ownership.ProvisionalAcquireIntent{{legacy, native}, {native, legacy}} {
			observed, err := journalqualification.ApplyIntentOwnershipEvidence(t.Context(), nil, intents, ownership.Registry{}, nil, nil)
			if !errors.Is(err, output.ErrNotLegacyFootprint) || observed != nil {
				t.Fatalf("old recovery observation returned partial or lossy evidence: %#v, %v", observed, err)
			}
		}
	}
}
