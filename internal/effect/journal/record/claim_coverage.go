package record

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

type claimCoveragePhase uint8

const (
	claimCoverageScanning claimCoveragePhase = iota + 1
	claimCoverageObserving
	claimCoverageComplete
	claimCoverageFinished
	claimCoverageFailed
)

type ClaimAuthorityDemand struct {
	index       int
	destination output.Destination
}

func (demand ClaimAuthorityDemand) Index() int { return demand.index }

func (demand ClaimAuthorityDemand) Destination() output.Destination { return demand.destination }

// A coverage session derives record facts. The capture shell supplies each
// observation before requesting another entry, preserving effect/error order.
type claimCoverageSession struct {
	phase                claimCoveragePhase
	entries              []recoveryEntry
	transitions          []ownershipmutation.ClaimTransition
	intents              []ownership.ProvisionalAcquireIntent
	transitionsByAddress map[string]struct{}
	intentKeys           map[string]struct{}
	cursor               int
}

func newClaimCoverageSession(entries []recoveryEntry, transitions []ownershipmutation.ClaimTransition, intents []ownership.ProvisionalAcquireIntent) (*claimCoverageSession, error) {
	if err := validateRecoveryOwnershipWorkBudget(len(entries), len(transitions), len(intents)); err != nil {
		return nil, err
	}
	session := &claimCoverageSession{
		phase: claimCoverageScanning, entries: slices.Clone(entries),
		transitions: slices.Clone(transitions), intents: slices.Clone(intents),
		transitionsByAddress: make(map[string]struct{}, len(transitions)),
		intentKeys:           make(map[string]struct{}, len(intents)),
	}
	for index, transition := range session.transitions {
		if err := transition.Validate(); err != nil {
			return nil, fmt.Errorf("recovery claim transitions[%d]: %w", index, err)
		}
		key := ownershipAddressKey(transition.Address())
		if _, duplicate := session.transitionsByAddress[key]; duplicate {
			return nil, fmt.Errorf("recovery journal has duplicate exact ownership transition")
		}
		session.transitionsByAddress[key] = struct{}{}
	}
	for index, intent := range session.intents {
		key, err := legacyProvisionalAcquireIntentKey(intent)
		if err != nil {
			return nil, fmt.Errorf("recovery provisional acquisition intents[%d]: %w", index, err)
		}
		if _, duplicate := session.intentKeys[key]; duplicate {
			return nil, fmt.Errorf("recovery journal has duplicate provisional acquisition intent")
		}
		session.intentKeys[key] = struct{}{}
	}
	return session, nil
}

func (session *claimCoverageSession) reject(err error) error {
	if session != nil {
		session.phase = claimCoverageFailed
	}
	return err
}

func (session *claimCoverageSession) next(ctx context.Context) (ClaimAuthorityDemand, bool, error) {
	if session == nil || session.phase != claimCoverageScanning {
		return ClaimAuthorityDemand{}, false, session.reject(fmt.Errorf("claim coverage cannot request another entry in its current phase"))
	}
	if ctx == nil {
		return ClaimAuthorityDemand{}, false, session.reject(fmt.Errorf("claim coverage context is required"))
	}
	for session.cursor < len(session.entries) {
		if err := ctx.Err(); err != nil {
			return ClaimAuthorityDemand{}, false, session.reject(err)
		}
		index := session.cursor
		entry := session.entries[index]
		if entry.OwnershipPathAuthority != nil {
			return ClaimAuthorityDemand{}, false, session.reject(fmt.Errorf("recovery entries[%d] capture already carries ownership path authority", index))
		}
		if !recoveryEntryRequiresOwnership(entry) {
			session.cursor++
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return ClaimAuthorityDemand{}, false, session.reject(fmt.Errorf("recovery entries[%d] destination: %w", index, err))
		}
		intentKey := provisionalAcquireIntentKey(destination, output.ContentPath(entry.ContentPath))
		if _, present := session.intentKeys[intentKey]; present {
			session.cursor++
			continue
		}
		session.phase = claimCoverageObserving
		return ClaimAuthorityDemand{index: index, destination: destination}, true, nil
	}
	session.phase = claimCoverageComplete
	return ClaimAuthorityDemand{}, false, nil
}

func (session *claimCoverageSession) bind(observation mutation.DirectoryEntryAuthorityObservation) error {
	if session == nil || session.phase != claimCoverageObserving {
		return session.reject(fmt.Errorf("claim coverage has no outstanding observation demand"))
	}
	index := session.cursor
	entry := session.entries[index]
	exact, present := observation.Exact()
	if !present {
		return session.reject(fmt.Errorf("recovery entries[%d] global output lacks exact ownership authority", index))
	}
	address, err := ownership.NewManagedAddressFromLegacy(exact, entry.ContentPath)
	if err != nil {
		return session.reject(fmt.Errorf("recovery entries[%d] ownership address: %w", index, err))
	}
	if _, present := session.transitionsByAddress[ownershipAddressKey(address)]; !present {
		return session.reject(fmt.Errorf("recovery entries[%d] global output has no exact ownership transition", index))
	}
	session.entries[index].OwnershipPathAuthority = persistedPathAuthority(exact)
	session.cursor++
	session.phase = claimCoverageScanning
	return nil
}

func (session *claimCoverageSession) finish() ([]recoveryEntry, error) {
	if session == nil || session.phase != claimCoverageComplete {
		return nil, session.reject(fmt.Errorf("claim coverage is incomplete or no longer usable"))
	}
	if err := validateRecoveryClaimCoverage(session.entries, session.transitions, session.intents); err != nil {
		return nil, session.reject(err)
	}
	session.phase = claimCoverageFinished
	return session.entries, nil
}
