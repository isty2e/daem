package operationplan

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
	"github.com/isty2e/daem/internal/output"
)

func TestActiveRecoveryFingerprintBindsNativeScope(t *testing.T) {
	footprints := []output.Footprint{output.WholePathFootprint()}
	for _, keys := range [][]string{{""}, {"a/b"}, {"a", "b"}, {"a~1b"}} {
		path, err := output.NewKeyPath(keys...)
		if err != nil {
			t.Fatal(err)
		}
		footprint, err := output.NewKeyFootprint(path)
		if err != nil {
			t.Fatal(err)
		}
		footprints = append(footprints, footprint)
	}
	var fingerprints []mutation.OperationFingerprint
	for _, footprint := range footprints {
		record, err := footprintcodec.Encode(footprint)
		if err != nil {
			t.Fatal(err)
		}
		input := ActiveRecoveryIdentityInput{ClaimTransitions: []ActiveRecoveryClaimTransition{{
			Kind: "retain", Footprint: record,
		}}}
		fingerprint, err := ActiveRecoveryOperationFingerprint(input)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := ActiveRecoveryOperationFingerprint(input)
		if err != nil || !repeated.Equal(fingerprint) {
			t.Fatalf("unchanged recovery scope changed its approval fingerprint: %v", err)
		}
		for _, previous := range fingerprints {
			if previous.Equal(fingerprint) {
				t.Fatal("different native scopes shared recovery approval")
			}
		}
		fingerprints = append(fingerprints, fingerprint)
	}
}
