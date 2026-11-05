package observation

import (
	"context"
	"fmt"
	"slices"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// Capture ownership derives claim/binding candidates from one selected read.
// Root custody, record encoding and publication remain operation-owned.
type nativeCaptureOwnershipInput struct {
	owner       stateauthority.Authority
	operationID string
	before      durable.Snapshot
	after       durable.Snapshot
	namespaces  []journalnamespace.DocumentNamespace
	limits      configdocument.Limits
	budget      *physicalwork.Budget
}

type nativeCaptureMembership struct {
	beforeNative     map[output.Destination]struct{}
	afterNative      map[output.Destination]struct{}
	beforeFootprints []map[output.Footprint]struct{}
	afterFootprints  []map[output.Footprint]struct{}
	missingHistory   []pathauthority.Provisional
}

func (input nativeCaptureOwnershipInput) membership(ctx context.Context) (nativeCaptureMembership, error) {
	base := CaptureInput{owner: input.owner, operationID: input.operationID, limits: input.limits, budget: input.budget}
	if err := base.Validate(ctx); err != nil {
		return nativeCaptureMembership{}, err
	}
	indexes, err := journaldocument.IndexSnapshots(ctx, input.before, input.after)
	if err != nil {
		return nativeCaptureMembership{}, err
	}
	beforeNative, err := indexes.Before().NativeDestinations(ctx)
	if err != nil {
		return nativeCaptureMembership{}, err
	}
	afterNative, err := indexes.After().NativeDestinations(ctx)
	if err != nil {
		return nativeCaptureMembership{}, err
	}
	beforeFootprints := make([]map[output.Footprint]struct{}, len(input.namespaces))
	afterFootprints := make([]map[output.Footprint]struct{}, len(input.namespaces))
	var missingHistory []pathauthority.Provisional
	seenAliases := make(map[aggregate.DocumentAddress]struct{})
	for index, namespace := range input.namespaces {
		aliases := namespace.Addresses()
		if len(aliases) == 0 {
			return nativeCaptureMembership{}, fmt.Errorf("native capture requires admitted document aliases")
		}
		if _, err := journaldocument.AliasSet(ctx, aliases); err != nil {
			return nativeCaptureMembership{}, err
		}
		if err := namespace.Observation().Validate(); err != nil {
			return nativeCaptureMembership{}, err
		}
		for _, alias := range aliases {
			if _, duplicate := seenAliases[alias]; duplicate {
				return nativeCaptureMembership{}, fmt.Errorf("native capture repeats a document alias")
			}
			seenAliases[alias] = struct{}{}
		}
		beforeFootprints[index], err = indexes.Before().Footprints(ctx, aliases)
		if err != nil {
			return nativeCaptureMembership{}, err
		}
		afterFootprints[index], err = indexes.After().Footprints(ctx, aliases)
		if err != nil {
			return nativeCaptureMembership{}, err
		}
		if missing, provisional := namespace.Observation().Provisional(); provisional && len(beforeFootprints[index]) != 0 {
			missingHistory = append(missingHistory, missing)
		}
	}
	return nativeCaptureMembership{
		beforeNative: beforeNative, afterNative: afterNative,
		beforeFootprints: beforeFootprints, afterFootprints: afterFootprints,
		missingHistory: missingHistory,
	}, nil
}

func (input nativeCaptureOwnershipInput) prepareFromRegistry(ctx context.Context, current ownership.Registry) ([]CaptureInput, error) {
	membership, err := input.membership(ctx)
	if err != nil {
		return nil, err
	}
	return input.derive(ctx, membership, current)
}

func (input nativeCaptureOwnershipInput) derive(ctx context.Context, membership nativeCaptureMembership, current ownership.Registry) ([]CaptureInput, error) {
	base := CaptureInput{owner: input.owner, operationID: input.operationID, limits: input.limits, budget: input.budget}
	owned := make(map[string][]ownership.Claim)
	for _, claim := range current.Claims() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if claim.OwnedBy(input.owner) {
			owned[claim.Address().Path()] = append(owned[claim.Address().Path()], claim)
		}
	}

	result := make([]CaptureInput, 0, len(input.namespaces))
	var preparations, finalizations []ownership.DocumentClaimChange
	var exactBindings []ownership.DocumentBindingIntent
	seenBindings := make(map[output.Destination]struct{})
	for index, namespace := range input.namespaces {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		capture := base
		capture.groupIndex = index
		var err error
		capture.claims, capture.provisional, err = input.documentClaims(ctx, namespace, current, owned, membership.beforeFootprints[index], membership.afterFootprints[index])
		if err != nil {
			return nil, err
		}
		capture.bindings, err = input.documentBindings(ctx, namespace, current, membership.beforeNative, membership.afterNative)
		if err != nil {
			return nil, err
		}
		for _, intent := range capture.bindings {
			if _, duplicate := seenBindings[intent.Destination()]; duplicate {
				return nil, fmt.Errorf("native groups repeat a binding key")
			}
			seenBindings[intent.Destination()] = struct{}{}
			if _, known := intent.KnownAfter(); known {
				exactBindings = append(exactBindings, intent)
			}
		}
		if capture.claims != nil {
			prepare, err := capture.claims.Preparation()
			if err != nil {
				return nil, err
			}
			finalize, err := capture.claims.Finalization()
			if err != nil {
				return nil, err
			}
			preparations = append(preparations, prepare)
			finalizations = append(finalizations, finalize)
		}
		result = append(result, capture)
	}

	for _, changes := range [][]ownership.DocumentClaimChange{preparations, finalizations} {
		convergence, err := ownership.NewClaimConvergence(nil)
		if err != nil {
			return nil, err
		}
		convergence, err = convergence.WithDocumentChanges(changes)
		if err != nil {
			return nil, err
		}
		current, _, err = convergence.Apply(current)
		if err != nil {
			return nil, err
		}
	}
	bindings, err := ownership.NewDocumentBindingConvergence(exactBindings)
	if err != nil {
		return nil, err
	}
	if _, _, err := bindings.Apply(current); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (input nativeCaptureOwnershipInput) documentClaims(ctx context.Context, namespace journalnamespace.DocumentNamespace, current ownership.Registry, owned map[string][]ownership.Claim, before, after map[output.Footprint]struct{}) (*ownershipmutation.DocumentClaimTransition, []journaldocument.ProvisionalAcquire, error) {
	if len(before)+len(after) == 0 {
		return nil, nil, nil
	}
	exact, beforeClaims, err := nativeDocumentClaimBaseline(namespace, owned, before)
	if err != nil {
		return nil, nil, err
	}
	if !exact.IsZero() {
		retained := make(map[output.Footprint]ownership.Claim, len(beforeClaims))
		for _, claim := range beforeClaims {
			retained[claim.Address().Footprint()] = claim
		}
		afterClaims := make([]ownership.Claim, 0, len(after))
		for footprint := range after {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
			claim, present := retained[footprint]
			if !present {
				address, err := ownership.NewManagedAddress(exact, footprint)
				if err != nil {
					return nil, nil, err
				}
				claim, err = ownership.NewActiveClaim(address, input.owner)
				if err != nil {
					return nil, nil, err
				}
			}
			afterClaims = append(afterClaims, claim)
		}
		transition, err := ownershipmutation.NewDocumentClaimTransition(beforeClaims, afterClaims, input.operationID)
		if err != nil {
			return nil, nil, err
		}
		return &transition, nil, nil
	}

	provisional, pending := namespace.Observation().Provisional()
	if !pending || len(before) != 0 {
		return nil, nil, fmt.Errorf("provisional native acquisition cannot reuse before membership")
	}
	if _, conflict := current.ProvisionalAncestorConflict(provisional); conflict {
		return nil, nil, fmt.Errorf("provisional native acquisition conflicts with current ownership")
	}
	var global aggregate.DocumentAddress
	for _, alias := range namespace.Addresses() {
		if alias.Scope() == target.ScopeGlobal {
			global = alias
			break
		}
	}
	acquires := make([]journaldocument.ProvisionalAcquire, 0, len(after))
	for footprint := range after {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		intent, err := ownership.NewProvisionalAcquireIntent(global.AggregateRoot(), footprint, provisional, input.owner, input.operationID)
		if err != nil {
			return nil, nil, err
		}
		acquires = append(acquires, journaldocument.NewProvisionalAcquire(global, intent))
	}
	slices.SortFunc(acquires, func(left, right journaldocument.ProvisionalAcquire) int {
		return output.CompareFootprints(left.Intent().Footprint(), right.Intent().Footprint())
	})
	return nil, acquires, nil
}

func (input nativeCaptureOwnershipInput) documentBindings(ctx context.Context, namespace journalnamespace.DocumentNamespace, current ownership.Registry, before, after map[output.Destination]struct{}) ([]ownership.DocumentBindingIntent, error) {
	intents := make([]ownership.DocumentBindingIntent, 0)
	seen := make(map[output.Destination]struct{})
	for _, alias := range namespace.Addresses() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		destination := alias.AggregateRoot()
		if _, duplicate := seen[destination]; duplicate {
			continue
		}
		seen[destination] = struct{}{}
		_, had := before[destination]
		_, has := after[destination]
		if !had && !has {
			continue
		}
		binding, present := current.DocumentBinding(input.owner.Key(), destination)
		if present != had {
			return nil, fmt.Errorf("native capture before membership differs from its current binding")
		}
		beforeValue, afterValue := ownership.NoDocumentBinding(), ownership.NoDocumentBinding()
		if had {
			if !namespace.Observation().MatchesRetainedExact(binding.PathAuthority()) {
				return nil, fmt.Errorf("native capture baseline binding differs from the captured namespace")
			}
			beforeValue, _ = ownership.PresentDocumentBinding(binding)
		}
		if has {
			if had {
				afterValue = beforeValue
			} else if exact, known := namespace.Observation().Exact(); known {
				var err error
				binding, err = ownership.NewDocumentBinding(input.owner, destination, exact)
				if err != nil {
					return nil, err
				}
				afterValue, _ = ownership.PresentDocumentBinding(binding)
			} else {
				pending, _ := namespace.Observation().Provisional()
				intent, err := ownership.NewProvisionalDocumentBindingIntent(input.owner, destination, pending)
				if err != nil {
					return nil, err
				}
				intents = append(intents, intent)
				continue
			}
		}
		intent, err := ownership.NewExactDocumentBindingIntent(input.owner, destination, beforeValue, afterValue)
		if err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, nil
}
