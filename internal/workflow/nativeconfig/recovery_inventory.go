package nativeconfig

import (
	"context"

	"github.com/isty2e/daem/internal/assurance/statefile"
	journal "github.com/isty2e/daem/internal/effect/journal/recoveryoperation"
)

// RecoveryInventoryProfile builds the production family decoders without
// loading the workspace's state or host configuration.
func RecoveryInventoryProfile(ctx context.Context) (journal.RecoveryInventoryProfile, error) {
	codecs, err := Codecs(ctx)
	if err != nil {
		return journal.RecoveryInventoryProfile{}, err
	}
	codec, err := statefile.NewNativeCodec(codecs.Contributions())
	if err != nil {
		return journal.RecoveryInventoryProfile{}, err
	}
	return journal.NewNativeRecoveryInventoryProfile(statefile.Codec{}, codec)
}
