package namespace

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

// ObserveWriterCapability consumes the acquired capability, closing it on every
// exit. Close or cancellation failures discard the observed path facts.
func ObserveWriterCapability(ctx context.Context, capability rootedpath.CommitCapability, budget *physicalwork.Budget) (observation mutation.DirectoryEntryAuthorityObservation, resultErr error) {
	if capability == nil {
		return mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf("writer observation requires an acquired capability")
	}
	defer func() {
		resultErr = errors.Join(resultErr, capability.Close(), ctx.Err())
		if resultErr != nil {
			observation = mutation.DirectoryEntryAuthorityObservation{}
		}
	}()
	if err := capability.Validate(); err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	path, err := capability.Destination().LexicalPath()
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	observation, err = mutation.ObserveDirectoryEntryAuthorityBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	return observation, errors.Join(err, capability.Validate())
}
