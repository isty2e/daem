package record

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/test/outputtest"
)

func TestLegacyJournalIntentRetainsLiteralTildeAndWholePath(t *testing.T) {
	base := testProvisionalAcquireIntent(t, globalAcquireRecoveryEntry(t))
	for _, spelling := range []output.ContentPath{"", "/literal~1", "/a/b"} {
		intent, err := ownership.NewProvisionalAcquireIntentFromLegacy(
			base.Destination(), spelling, base.Path(), base.Owner(), base.OperationID(),
		)
		if err != nil {
			t.Fatal(err)
		}
		persisted, err := recoveryProvisionalAcquireIntents([]ownership.ProvisionalAcquireIntent{intent})
		if err != nil || len(persisted) != 1 || persisted[0].ContentPath != string(spelling) {
			t.Fatalf("legacy intent spelling changed: %#v, %v", persisted, err)
		}
		decoded, err := canonicalProvisionalAcquireIntents(persisted)
		if err != nil || len(decoded) != 1 || !decoded[0].Equal(intent) {
			t.Fatalf("legacy intent identity changed on round trip: %#v, %v", decoded, err)
		}
	}
}

func TestLegacyJournalIntentFrontiersRefuseNativeScopesWithoutPartialResults(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	legacy := testProvisionalAcquireIntent(t, entry)
	for _, segments := range [][]string{{""}, {"a/b"}, {"."}, {"line\n\x00key"}} {
		native, err := ownership.NewProvisionalAcquireIntent(
			legacy.Destination(), outputtest.KeyFootprint(t, segments...),
			legacy.Path(), legacy.Owner(), legacy.OperationID(),
		)
		if err != nil {
			t.Fatal(err)
		}
		for _, intents := range [][]ownership.ProvisionalAcquireIntent{{legacy, native}, {native, legacy}} {
			encoded, err := recoveryProvisionalAcquireIntents(intents)
			if !errors.Is(err, output.ErrNotLegacyFootprint) || encoded != nil {
				t.Fatalf("old journal encoder returned partial or lossy records: %#v, %v", encoded, err)
			}
			resolverCalls := 0
			bound, err := bindRecoveryClaimCoverage(t.Context(), []recoveryEntry{entry}, nil, intents, func(output.Destination) (string, error) {
				resolverCalls++
				return "", errors.New("unexpected resolution")
			})
			if !errors.Is(err, output.ErrNotLegacyFootprint) || bound != nil || resolverCalls != 0 {
				t.Fatalf("old journal coverage admitted native scope or performed resolution: %#v, %d, %v", bound, resolverCalls, err)
			}
			if err := validateRecoveryClaimCoverage([]recoveryEntry{entry}, nil, intents); !errors.Is(err, output.ErrNotLegacyFootprint) {
				t.Fatalf("old journal validation admitted native scope: %v", err)
			}
		}
		if index, err := promotedIntentEntryIndex([]recoveryEntry{entry}, native); !errors.Is(err, output.ErrNotLegacyFootprint) || index != -1 {
			t.Fatalf("old journal promotion matched a lossy scope: %d, %v", index, err)
		}
	}
}
