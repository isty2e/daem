package protection

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
)

// DocumentAccess requalifies a document's complete cohort and returns a current
// rooted destination. The caller owns the returned root and the supplied budget.
type DocumentAccess interface {
	CaptureDocument(context.Context, ownership.ProtectedDocument, rootedpath.PhysicalTraversalBudget) (*rootedpath.CapturedRoot, rootedpath.Destination, error)
}

type Containment struct {
	access DocumentAccess
	budget rootedpath.PhysicalTraversalBudget
}

func NewContainment(access DocumentAccess, budget rootedpath.PhysicalTraversalBudget) Containment {
	return Containment{access: access, budget: budget}
}

func (containment Containment) InspectDocument(ctx context.Context, document ownership.ProtectedDocument, policy storagecommit.PermissionRestrictionPolicy) (observation storagecommit.PermissionRestrictionResult, returnErr error) {
	root, destination, err := containment.access.CaptureDocument(ctx, document, containment.budget)
	if err != nil {
		return observation, err
	}
	defer func() { returnErr = errors.Join(returnErr, root.Close()) }()
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, containment.budget)
	if err != nil {
		return observation, err
	}
	request, err := storagecommit.NewRootedPermissionRestriction(capability, policy)
	if err != nil {
		return observation, errors.Join(err, capability.Close())
	}
	observation, err = storagecommit.InspectRootedFilePermissions(ctx, request)
	return observation, Refusal(MetadataUnqualified, err)
}

func (containment Containment) Assess(ctx context.Context, registry ownership.Registry, row ownership.PendingProtection) Record {
	record := Record{invocation: row.Invocation(), phase: row.Phase(), settlement: SettlementPending}
	processErr := RequireQuiescence(ctx, row)
	for _, document := range row.Documents() {
		target := Target{destination: document.Bindings()[0].Destination(), state: TargetCheck, cause: processErr}
		if target.cause == nil {
			target.cause = registry.RequireProtectedDocument(row, document)
			if target.cause == nil {
				observation, err := containment.InspectDocument(ctx, document, document.Restriction)
				target.expanded, target.cause = observation.Expanded(), err
			}
		}
		if target.cause != nil {
			target.state = TargetBlocked
		}
		record.targets = append(record.targets, target)
	}
	return record
}

// Settle restricts independently eligible targets and clears only a fully proved
// record. The containing operation supplies its existing authority checks.
func (containment Containment) Settle(ctx context.Context, store ownershipstore.Store, row ownership.PendingProtection, record Record, eligibility map[string]error, beforeTarget, beforeClear func(context.Context) error) Record {
	record.targets = slices.Clone(record.targets)
	record.phase = row.Phase()
	record.settlement = SettlementPending
	if !containment.restrictTargets(ctx, store, row, &record, eligibility, beforeTarget) {
		return record
	}
	if beforeClear != nil {
		record.cause = beforeClear(ctx)
	}
	if record.cause == nil {
		record.settlement, record.cause = settleProtection(ctx, store, row.Owner(), row)
	}
	return record
}

func RequireQuiescence(ctx context.Context, row ownership.PendingProtection) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	binding, bound := row.Execution()
	if !bound {
		return nil
	}
	observation, err := executioncontext.ObserveExecutionQuiescence(ctx, binding)
	if err != nil {
		return err
	}
	if err := observation.Require(binding); err != nil {
		return fmt.Errorf("permission recovery quiescence: %w", err)
	}
	return nil
}
