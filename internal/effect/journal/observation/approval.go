package observation

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

// ApprovalEvidence projects stable namespace and protection facts.
// It excludes content revisions and final-file object identity and grants no effects.
type ApprovalEvidence struct {
	Aliases         []aggregate.DocumentAddress
	Authority       mutation.DirectoryEntryAuthorityObservation
	Protection      string
	Roles           []configuration.DocumentSpec
	GlobalLocations []output.RootReference
	requests        map[aggregate.DocumentAddress]mutation.PhysicalAuthorityRequest
	removal         journalnamespace.RemovalPreflight
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

func (evidence ApprovalEvidence) RequireRecoverableRemoval(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) error {
	return evidence.removal.RequireRecoverablePath(ctx, budget)
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
			identity, supported := original.identity.(interface{ OwnershipFingerprint() (string, error) })
			if !supported {
				return nil, fmt.Errorf("selected filesystem cannot project file protection for replacement approval")
			}
			var err error
			protection, err = identity.OwnershipFingerprint()
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
		result = append(result, ApprovalEvidence{
			Aliases: namespace.Addresses(), Authority: namespace.Observation(), Protection: protection,
			Roles: namespace.PhysicalRoles(), GlobalLocations: namespace.GlobalLocations(), requests: requests,
			removal: namespace.RemovalPreflight(),
		})
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
