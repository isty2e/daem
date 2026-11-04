package journal

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// NativeDocumentApprovalEvidence projects stable namespace and protection facts.
// It excludes content revisions and final-file object identity and grants no effects.
type NativeDocumentApprovalEvidence struct {
	Aliases    []aggregate.DocumentAddress
	Authority  mutation.DirectoryEntryAuthorityObservation
	Protection string
}

func (batch NativeDocumentObservationBatch) ApprovalEvidence() ([]NativeDocumentApprovalEvidence, error) {
	if !batch.initialized {
		return nil, fmt.Errorf("native observation is unavailable")
	}
	result := make([]NativeDocumentApprovalEvidence, 0, len(batch.namespaces))
	for index, namespace := range batch.namespaces {
		original := batch.originals[index]
		protection := "absent"
		if original.document.Exists() {
			identity, supported := original.identity.(interface{ ProtectionFingerprint() (string, error) })
			if !supported {
				return nil, fmt.Errorf("selected filesystem cannot project file protection for replacement approval")
			}
			var err error
			protection, err = identity.ProtectionFingerprint()
			if err != nil {
				return nil, err
			}
		}
		result = append(result, NativeDocumentApprovalEvidence{Aliases: namespace.Addresses(), Authority: namespace.Observation(), Protection: protection})
	}
	return result, nil
}

// HasMembers identifies documents retained by the complete Before/After state.
// An empty declaration alone is not a document actor.
func (evidence NativeDocumentApprovalEvidence) HasMembers(ctx context.Context, before, after durable.Snapshot) (bool, error) {
	indexes, err := journaldocument.IndexSnapshots(ctx, before, after)
	if err != nil {
		return false, err
	}
	return indexes.HasMembers(ctx, evidence.Aliases)
}
