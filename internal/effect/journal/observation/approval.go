package observation

import (
	"context"
	"fmt"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// ApprovalEvidence projects stable namespace and protection facts.
// It excludes content revisions and final-file object identity and grants no effects.
type ApprovalEvidence struct {
	Aliases    []aggregate.DocumentAddress
	Authority  mutation.DirectoryEntryAuthorityObservation
	Protection string
	requests   map[aggregate.DocumentAddress]mutation.PhysicalAuthorityRequest
}

// AuthorityRequest retains qualified routing facts after observation resources
// close. The request still requires the containing operation's current leases.
func (evidence ApprovalEvidence) AuthorityRequest(alias aggregate.DocumentAddress) (mutation.PhysicalAuthorityRequest, error) {
	request, present := evidence.requests[alias]
	if !present {
		return mutation.PhysicalAuthorityRequest{}, fmt.Errorf("native approval has no qualified authority request for the selected document")
	}
	return request, nil
}

func (batch Batch) ApprovalEvidence() ([]ApprovalEvidence, error) {
	if !batch.initialized {
		return nil, fmt.Errorf("native observation is unavailable")
	}
	result := make([]ApprovalEvidence, 0, len(batch.namespaces))
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
		requests := make(map[aggregate.DocumentAddress]mutation.PhysicalAuthorityRequest)
		for _, alias := range namespace.Aliases() {
			request, err := alias.PhysicalAuthorityRequest()
			if err != nil {
				return nil, err
			}
			requests[alias.Address()] = request
		}
		result = append(result, ApprovalEvidence{Aliases: namespace.Addresses(), Authority: namespace.Observation(), Protection: protection, requests: requests})
	}
	return result, nil
}

// HasMembers identifies documents retained by the complete Before/After state.
// An empty declaration alone is not a document actor.
func (evidence ApprovalEvidence) HasMembers(ctx context.Context, before, after durable.Snapshot) (bool, error) {
	indexes, err := journaldocument.IndexSnapshotsForClaims(ctx, before, after, journaldocument.NativeClaims)
	if err != nil {
		return false, err
	}
	return indexes.HasMembers(ctx, evidence.Aliases)
}
