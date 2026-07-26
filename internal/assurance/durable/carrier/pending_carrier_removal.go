package carrier

import (
	"cmp"
	"fmt"
	"sort"

	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/supply/artifact"
)

// EffectBaselineState identifies the pre-effect state needed to verify one
// route-coupled invariant after an interrupted removal.
type EffectBaselineState string

const (
	EffectBaselineAbsent  EffectBaselineState = "absent"
	EffectBaselineContent EffectBaselineState = "content"
)

// EffectBaseline is one path-content fact captured before a delegated effect.
// It contains no host path; the exact claim remains the authority for deriving
// the path at observation time.
type EffectBaseline struct {
	requirement effectpostcondition.Requirement
	state       EffectBaselineState
	contentHash artifact.ContentHash
}

// NewAbsentEffectBaseline records that the selected path did not exist before
// the delegated effect.
func NewAbsentEffectBaseline(
	requirement effectpostcondition.Requirement,
) (EffectBaseline, error) {
	return newEffectBaseline(requirement, EffectBaselineAbsent, "")
}

// NewContentEffectBaseline records the exact file or directory content that
// existed before the delegated effect.
func NewContentEffectBaseline(
	requirement effectpostcondition.Requirement,
	contentHash artifact.ContentHash,
) (EffectBaseline, error) {
	return newEffectBaseline(requirement, EffectBaselineContent, contentHash)
}

func newEffectBaseline(
	requirement effectpostcondition.Requirement,
	state EffectBaselineState,
	contentHash artifact.ContentHash,
) (EffectBaseline, error) {
	if requirement != effectpostcondition.LocalSourceUnchanged {
		return EffectBaseline{}, fmt.Errorf(
			"effect baseline requirement %q does not admit pre-effect content",
			requirement,
		)
	}
	switch state {
	case EffectBaselineAbsent:
		if contentHash != "" {
			return EffectBaseline{}, fmt.Errorf("absent effect baseline must not carry content")
		}
	case EffectBaselineContent:
		if err := contentHash.Validate(); err != nil {
			return EffectBaseline{}, fmt.Errorf("effect baseline content hash: %w", err)
		}
	default:
		return EffectBaseline{}, fmt.Errorf("effect baseline state %q is unsupported", state)
	}
	return EffectBaseline{
		requirement: requirement,
		state:       state,
		contentHash: contentHash,
	}, nil
}

// Requirement returns the exact route-coupled predicate this baseline supports.
func (baseline EffectBaseline) Requirement() effectpostcondition.Requirement {
	return baseline.requirement
}

// State returns whether the selected path was absent or had exact content.
func (baseline EffectBaseline) State() EffectBaselineState { return baseline.state }

// ContentHash returns the exact pre-effect content identity when State is content.
func (baseline EffectBaseline) ContentHash() (artifact.ContentHash, bool) {
	return baseline.contentHash, baseline.state == EffectBaselineContent
}

func (baseline EffectBaseline) validate() error {
	expected, err := newEffectBaseline(
		baseline.requirement,
		baseline.state,
		baseline.contentHash,
	)
	if err != nil {
		return err
	}
	if baseline != expected {
		return fmt.Errorf("effect baseline is not canonical")
	}
	return nil
}

// EffectBaselineSet is one canonical requirement-indexed pre-effect baseline
// collection. The zero value is an explicit empty set.
type EffectBaselineSet struct {
	baselines []EffectBaseline
}

// NewEffectBaselineSet validates, sorts, and copies pre-effect baselines.
func NewEffectBaselineSet(values []EffectBaseline) (EffectBaselineSet, error) {
	baselines := append([]EffectBaseline(nil), values...)
	for index, baseline := range baselines {
		if err := baseline.validate(); err != nil {
			return EffectBaselineSet{}, fmt.Errorf("effect baseline[%d]: %w", index, err)
		}
	}
	sort.Slice(baselines, func(left int, right int) bool {
		return baselines[left].requirement < baselines[right].requirement
	})
	for index := 1; index < len(baselines); index++ {
		if baselines[index-1].requirement == baselines[index].requirement {
			return EffectBaselineSet{}, fmt.Errorf(
				"effect baseline requirement %q is duplicated",
				baselines[index].requirement,
			)
		}
	}
	return EffectBaselineSet{baselines: baselines}, nil
}

// Validate rejects forged or non-canonical baseline sets.
func (set EffectBaselineSet) Validate() error {
	expected, err := NewEffectBaselineSet(set.baselines)
	if err != nil {
		return err
	}
	if !set.Equal(expected) {
		return fmt.Errorf("effect baseline set is not canonical")
	}
	return nil
}

// Baselines returns a defensive copy in canonical requirement order.
func (set EffectBaselineSet) Baselines() []EffectBaseline {
	return append([]EffectBaseline(nil), set.baselines...)
}

// For returns the unique baseline for one requirement.
func (set EffectBaselineSet) For(
	requirement effectpostcondition.Requirement,
) (EffectBaseline, bool) {
	for _, baseline := range set.baselines {
		if baseline.requirement == requirement {
			return baseline, true
		}
	}
	return EffectBaseline{}, false
}

// Equal reports exact canonical set equality.
func (set EffectBaselineSet) Equal(other EffectBaselineSet) bool {
	if len(set.baselines) != len(other.baselines) {
		return false
	}
	for index := range set.baselines {
		if set.baselines[index] != other.baselines[index] {
			return false
		}
	}
	return true
}

// PendingCarrierRemoval is a write-ahead boundary for one exact managed claim
// and one admitted remove request. It is not current host evidence and does not
// by itself authorize a host invocation or direct mutation.
type PendingCarrierRemoval struct {
	claim                ManagedCarrierClaim
	removeRequest        realizationdelegate.Request
	effectPostconditions effectpostcondition.Set
	effectBaselines      EffectBaselineSet
}

// NewPendingCarrierRemoval constructs exact pre-effect removal state.
func NewPendingCarrierRemoval(
	claim ManagedCarrierClaim,
	removeRequest realizationdelegate.Request,
	effectPostconditions effectpostcondition.Set,
	effectBaselines EffectBaselineSet,
) (PendingCarrierRemoval, error) {
	pending := PendingCarrierRemoval{
		claim:                claim,
		removeRequest:        removeRequest,
		effectPostconditions: effectPostconditions,
		effectBaselines:      effectBaselines,
	}
	if err := pending.Validate(); err != nil {
		return PendingCarrierRemoval{}, err
	}
	return pending, nil
}

// Validate rejects incomplete or mixed pending-removal facts.
func (pending PendingCarrierRemoval) Validate() error {
	if err := pending.claim.Validate(); err != nil {
		return fmt.Errorf("pending carrier removal claim: %w", err)
	}
	if err := pending.removeRequest.Validate(); err != nil {
		return fmt.Errorf("pending carrier removal request: %w", err)
	}
	if err := pending.effectPostconditions.Validate(); err != nil {
		return fmt.Errorf("pending carrier removal effect postconditions: %w", err)
	}
	if err := pending.effectBaselines.Validate(); err != nil {
		return fmt.Errorf("pending carrier removal effect baselines: %w", err)
	}
	if err := validateEffectBaselines(
		pending.effectPostconditions,
		pending.effectBaselines,
	); err != nil {
		return fmt.Errorf("pending carrier removal: %w", err)
	}
	return nil
}

// Claim returns the exact durable deletion authority prepared for removal.
func (pending PendingCarrierRemoval) Claim() ManagedCarrierClaim {
	return pending.claim
}

// Owner returns the state authority that prepared the removal.
func (pending PendingCarrierRemoval) Owner() StateAuthority {
	return pending.claim.Owner()
}

// Identity returns the exact carrier and relation identity.
func (pending PendingCarrierRemoval) Identity() ManagedCarrierIdentity {
	return pending.claim.Identity()
}

// RemoveRequest returns the admitted removal route identity.
func (pending PendingCarrierRemoval) RemoveRequest() realizationdelegate.Request {
	return pending.removeRequest
}

// EffectPostconditions returns the exact locked coupled predicates that must
// be freshly satisfied before this claim may be retired.
func (pending PendingCarrierRemoval) EffectPostconditions() effectpostcondition.Set {
	return pending.effectPostconditions
}

// EffectBaselines returns immutable pre-effect facts required for interrupted
// postcondition verification.
func (pending PendingCarrierRemoval) EffectBaselines() EffectBaselineSet {
	return pending.effectBaselines
}

// FactKey returns the owner-relation key used for durable collection identity.
func (pending PendingCarrierRemoval) FactKey() CarrierFactKey {
	return pending.claim.FactKey()
}

// Compare returns the canonical persisted order between pending removals.
func (pending PendingCarrierRemoval) Compare(other PendingCarrierRemoval) int {
	return cmp.Or(
		cmp.Compare(pending.Owner().StatefileKey(), other.Owner().StatefileKey()),
		cmp.Compare(pending.Identity().RelationSubject().String(), other.Identity().RelationSubject().String()),
		cmp.Compare(pending.Identity().CarrierSubject().String(), other.Identity().CarrierSubject().String()),
		cmp.Compare(pending.removeRequest.RouteID(), other.removeRequest.RouteID()),
		cmp.Compare(pending.removeRequest.ContractVersion(), other.removeRequest.ContractVersion()),
		cmp.Compare(pending.removeRequest.CanonicalRequestHash(), other.removeRequest.CanonicalRequestHash()),
	)
}

// ExactEqual reports complete persisted pending-state equality.
func (pending PendingCarrierRemoval) ExactEqual(other PendingCarrierRemoval) bool {
	return pending.claim.ExactEqual(other.claim) &&
		pending.removeRequest.Equal(other.removeRequest) &&
		pending.effectPostconditions.Equal(other.effectPostconditions) &&
		pending.effectBaselines.Equal(other.effectBaselines)
}

func validateEffectBaselines(
	requirements effectpostcondition.Set,
	baselines EffectBaselineSet,
) error {
	required := make(map[effectpostcondition.Requirement]struct{})
	for _, requirement := range requirements.Requirements() {
		required[requirement] = struct{}{}
	}
	for _, baseline := range baselines.baselines {
		if _, present := required[baseline.requirement]; !present {
			return fmt.Errorf(
				"effect baseline %q has no matching postcondition",
				baseline.requirement,
			)
		}
	}
	_, localBaselinePresent := baselines.For(effectpostcondition.LocalSourceUnchanged)
	_, localRequirementPresent := required[effectpostcondition.LocalSourceUnchanged]
	if localRequirementPresent != localBaselinePresent {
		return fmt.Errorf(
			"local source unchanged postcondition requires exactly one pre-effect baseline",
		)
	}
	return nil
}
