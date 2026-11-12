//go:build !darwin && !linux

package commit

import (
	"context"
	"fmt"
)

// ConfirmFilePersistence has no admitted implementation on this platform.
func ConfirmFilePersistence(ctx context.Context, path string, _ EntryIdentity) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return newFailure(failureUnsupportedGuarantee, phaseUnsupported, path, fmt.Errorf("file persistence confirmation is unsupported"))
}
