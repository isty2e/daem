package recovery

import (
	"fmt"
	"path"
	"slices"
	"strings"

	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// RemovalNamespaceVariant identifies the parent namespace relation captured at
// journal publication. Existing and initially absent parents are intentionally
// different authorities.
type RemovalNamespaceVariant string

const (
	RemovalNamespaceExistingParent        RemovalNamespaceVariant = "existing_parent"
	RemovalNamespaceInitiallyAbsentParent RemovalNamespaceVariant = "initially_absent_parent"
)

// RemovalNamespaceAuthority is durable relation-specific namespace evidence.
// It grants no filesystem capability and owns no cleanup policy.
type RemovalNamespaceAuthority struct {
	variant          RemovalNamespaceVariant
	parent           ManifestRootProvenance
	retainedAncestor ManifestRootProvenance
	missingSuffix    string
	names            mutationfs.LogicalRemovalNames
}

// NewExistingParentAuthority binds a residue to the exact parent incarnation
// and retains the nearest ancestor needed to prove durable absence if that
// parent disappears before reconciliation.
func NewExistingParentAuthority(
	parent ManifestRootProvenance,
	retainedAncestor ManifestRootProvenance,
	missingSuffix string,
	names mutationfs.LogicalRemovalNames,
) (RemovalNamespaceAuthority, error) {
	if err := parent.validate(); err != nil {
		return RemovalNamespaceAuthority{}, fmt.Errorf("existing parent provenance: %w", err)
	}
	if err := retainedAncestor.validate(); err != nil {
		return RemovalNamespaceAuthority{}, fmt.Errorf("existing parent retained ancestor provenance: %w", err)
	}
	if err := validateParentRelation(parent, retainedAncestor, missingSuffix); err != nil {
		return RemovalNamespaceAuthority{}, fmt.Errorf("existing parent relation: %w", err)
	}
	if !names.Valid() {
		return RemovalNamespaceAuthority{}, fmt.Errorf("existing parent removal names are invalid")
	}
	return RemovalNamespaceAuthority{
		variant:          RemovalNamespaceExistingParent,
		parent:           parent,
		retainedAncestor: retainedAncestor,
		missingSuffix:    missingSuffix,
		names:            names,
	}, nil
}

// NewInitiallyAbsentParentAuthority binds a missing parent suffix to its
// nearest existing retained ancestor without authorizing parent creation.
func NewInitiallyAbsentParentAuthority(
	retainedAncestor ManifestRootProvenance,
	missingSuffix string,
	names mutationfs.LogicalRemovalNames,
) (RemovalNamespaceAuthority, error) {
	if err := retainedAncestor.validate(); err != nil {
		return RemovalNamespaceAuthority{}, fmt.Errorf("retained ancestor provenance: %w", err)
	}
	if strings.TrimSpace(missingSuffix) != missingSuffix || !canonicalRelativeSuffix(missingSuffix) {
		return RemovalNamespaceAuthority{}, fmt.Errorf("missing parent suffix must be canonical and relative")
	}
	if !names.Valid() {
		return RemovalNamespaceAuthority{}, fmt.Errorf("initially absent parent removal names are invalid")
	}
	return RemovalNamespaceAuthority{
		variant:          RemovalNamespaceInitiallyAbsentParent,
		retainedAncestor: retainedAncestor,
		missingSuffix:    missingSuffix,
		names:            names,
	}, nil
}

func canonicalRelativeSuffix(value string) bool {
	if value == "" || path.IsAbs(value) || path.Clean(value) != value || value == "." || value == ".." || strings.HasPrefix(value, "../") {
		return false
	}
	return !strings.ContainsRune(value, '\x00') && !strings.Contains(value, "\\")
}

// Variant returns the closed namespace-authority variant.
func (authority RemovalNamespaceAuthority) Variant() RemovalNamespaceVariant {
	return authority.variant
}

// ParentProvenance returns the exact initially-existing parent evidence.
func (authority RemovalNamespaceAuthority) ParentProvenance() (ManifestRootProvenance, bool) {
	return authority.parent, authority.variant == RemovalNamespaceExistingParent
}

// RetainedAncestorProvenance returns the nearest retained ancestor evidence.
func (authority RemovalNamespaceAuthority) RetainedAncestorProvenance() (ManifestRootProvenance, bool) {
	return authority.retainedAncestor, authority.retainedAncestor != (ManifestRootProvenance{})
}

// MissingSuffix returns the canonical missing-parent suffix, when applicable.
func (authority RemovalNamespaceAuthority) MissingSuffix() string { return authority.missingSuffix }

// Names returns the exact pre-effect residue and cleanup-stage namespace slots.
func (authority RemovalNamespaceAuthority) Names() mutationfs.LogicalRemovalNames {
	return authority.names
}

func (authority RemovalNamespaceAuthority) validate() error {
	switch authority.variant {
	case RemovalNamespaceExistingParent:
		if err := authority.parent.validate(); err != nil {
			return err
		}
		if err := authority.retainedAncestor.validate(); err != nil {
			return fmt.Errorf("existing parent retained ancestor: %w", err)
		}
		if err := validateParentRelation(authority.parent, authority.retainedAncestor, authority.missingSuffix); err != nil {
			return err
		}
	case RemovalNamespaceInitiallyAbsentParent:
		if err := authority.retainedAncestor.validate(); err != nil {
			return err
		}
		if !canonicalRelativeSuffix(authority.missingSuffix) {
			return fmt.Errorf("initially absent parent suffix is not canonical")
		}
		if authority.parent != (ManifestRootProvenance{}) {
			return fmt.Errorf("initially absent parent authority must not carry parent facts")
		}
	default:
		return fmt.Errorf("unsupported removal namespace variant %q", authority.variant)
	}
	if !authority.names.Valid() {
		return fmt.Errorf("removal namespace names are invalid")
	}
	return nil
}

func validateParentRelation(
	parent ManifestRootProvenance,
	retainedAncestor ManifestRootProvenance,
	missingSuffix string,
) error {
	if !canonicalRelativeSuffix(missingSuffix) {
		return fmt.Errorf("parent suffix must be canonical and relative")
	}
	ancestor := strings.TrimSuffix(retainedAncestor.PhysicalRoot(), "/")
	prefix := ancestor + "/"
	if !strings.HasPrefix(parent.PhysicalRoot(), prefix) || strings.TrimPrefix(parent.PhysicalRoot(), prefix) != missingSuffix {
		return fmt.Errorf("parent is not the captured descendant %q of retained ancestor", missingSuffix)
	}
	return nil
}

func (authority RemovalNamespaceAuthority) equal(other RemovalNamespaceAuthority) bool {
	return authority.variant == other.variant &&
		authority.parent.Equal(other.parent) &&
		authority.retainedAncestor.Equal(other.retainedAncestor) &&
		authority.missingSuffix == other.missingSuffix &&
		authority.names.Equal(other.names)
}

// RemovalState is one complete whole-path state that a journaled removal may
// move into the selected residue. It deliberately keeps before and
// expected-after semantics out of the physical cleanup layer.
type RemovalState struct {
	before   *BeforePathState
	expected *ExpectedPathState
}

// NewBeforeRemovalState admits one complete pre-action path state.
func NewBeforeRemovalState(state BeforePathState) (RemovalState, error) {
	if err := validateRemovalBeforeState(state); err != nil {
		return RemovalState{}, fmt.Errorf("before removal state: %w", err)
	}
	copy := cloneBeforePathState(state)
	// A backup location is recovery representation, not a fact about the
	// removable rooted entry. Keeping it out of the demand basis prevents
	// capture-time backup allocation from changing execute-time authority.
	copy.BackupPath = ""
	return RemovalState{before: &copy}, nil
}

// NewExpectedRemovalState admits one complete post-action path state.
func NewExpectedRemovalState(state ExpectedPathState) (RemovalState, error) {
	if err := validateRemovalExpectedState(state); err != nil {
		return RemovalState{}, fmt.Errorf("expected removal state: %w", err)
	}
	copy := state.Clone()
	return RemovalState{expected: &copy}, nil
}

func validateRemovalBeforeState(state BeforePathState) error {
	if !state.Existed {
		if state.Kind != "" || state.ContentHash != "" || state.BackupPath != "" || state.LinkTarget != "" {
			return fmt.Errorf("absent state carries existing-entry facts")
		}
		if !state.PathExisted && state.ParentExisted {
			return fmt.Errorf("absent state parent existence requires path existence")
		}
		if !state.PathExisted && state.PathMode != nil {
			return fmt.Errorf("absent state mode requires path existence")
		}
		return nil
	}
	return validateRemovalExistingFacts(state.Kind, state.ContentHash, state.BackupPath, state.LinkTarget, state.PathMode)
}

func validateRemovalExpectedState(state ExpectedPathState) error {
	if !state.Existed {
		if state.Kind != "" || state.ContentHash != "" || state.LinkTarget != "" {
			return fmt.Errorf("absent state carries existing-entry facts")
		}
		if !state.PathExisted && state.PathMode != nil {
			return fmt.Errorf("absent state mode requires path existence")
		}
		return nil
	}
	return validateRemovalExistingFacts(state.Kind, state.ContentHash, "", state.LinkTarget, state.PathMode)
}

func validateRemovalExistingFacts(kind, contentHash, backupPath, linkTarget string, mode *PermissionMode) error {
	switch kind {
	case PathKindFile, PathKindDirectory:
		if strings.TrimSpace(contentHash) == "" {
			return fmt.Errorf("existing state content hash is required")
		}
		if kind == PathKindDirectory && mode != nil {
			return fmt.Errorf("directory state must not carry a file mode")
		}
		if kind == PathKindFile && mode == nil {
			return fmt.Errorf("file state mode is required")
		}
		if linkTarget != "" {
			return fmt.Errorf("regular state must not carry a link target")
		}
		if backupPath != "" && !isSafeRelativePath(backupPath) {
			return fmt.Errorf("state backup path is not canonical")
		}
	case PathKindSymlink:
		if strings.TrimSpace(linkTarget) == "" || contentHash != "" || backupPath != "" || mode != nil {
			return fmt.Errorf("symlink state carries incompatible facts")
		}
	default:
		return fmt.Errorf("unsupported removal state kind %q", kind)
	}
	return nil
}

// Before returns the admitted before state, when this is a before candidate.
func (state RemovalState) Before() (BeforePathState, bool) {
	if state.before == nil {
		return BeforePathState{}, false
	}
	return cloneBeforePathState(*state.before), true
}

// Expected returns the admitted expected-after state, when this is an expected candidate.
func (state RemovalState) Expected() (ExpectedPathState, bool) {
	if state.expected == nil {
		return ExpectedPathState{}, false
	}
	return state.expected.Clone(), true
}

func (state RemovalState) equal(other RemovalState) bool {
	before, beforeOK := state.Before()
	otherBefore, otherBeforeOK := other.Before()
	if beforeOK != otherBeforeOK || (beforeOK && !beforeEqual(before, otherBefore)) {
		return false
	}
	expected, expectedOK := state.Expected()
	otherExpected, otherExpectedOK := other.Expected()
	return expectedOK == otherExpectedOK && (!expectedOK || expected.Equal(otherExpected))
}

// Equal compares two complete admitted whole-path states.
func (state RemovalState) Equal(other RemovalState) bool { return state.equal(other) }

func beforeEqual(left, right BeforePathState) bool {
	if left.Existed != right.Existed || left.PathExisted != right.PathExisted || left.ParentExisted != right.ParentExisted || left.Kind != right.Kind || left.ContentHash != right.ContentHash || left.LinkTarget != right.LinkTarget {
		return false
	}
	if left.PathMode == nil || right.PathMode == nil {
		return left.PathMode == nil && right.PathMode == nil
	}
	return *left.PathMode == *right.PathMode
}

// RemovalDemand is the pure transition-derived basis shared by capture and
// execute coverage. It identifies one rooted relation and all complete states
// that a journaled removal may legally move into the residue.
type RemovalDemand struct {
	scope       target.Scope
	destination output.Destination
	states      []RemovalState
}

// NewRemovalDemand constructs one canonical transition demand.
func NewRemovalDemand(scope target.Scope, destination output.Destination, states []RemovalState) (RemovalDemand, error) {
	if err := destination.ValidateScope(scope); err != nil {
		return RemovalDemand{}, fmt.Errorf("removal demand relation: %w", err)
	}
	if len(states) == 0 {
		return RemovalDemand{}, fmt.Errorf("removal demand requires at least one admitted state")
	}
	copy := append([]RemovalState(nil), states...)
	for index, state := range copy {
		if state.before == nil && state.expected == nil {
			return RemovalDemand{}, fmt.Errorf("removal demand state[%d] is empty", index)
		}
		for prior := 0; prior < index; prior++ {
			if copy[prior].equal(state) {
				return RemovalDemand{}, fmt.Errorf("removal demand contains duplicate admitted state")
			}
		}
	}
	slices.SortFunc(copy, func(left, right RemovalState) int {
		return strings.Compare(removalStateSortKey(left), removalStateSortKey(right))
	})
	return RemovalDemand{scope: scope, destination: destination, states: copy}, nil
}

func removalStateSortKey(state RemovalState) string {
	if before, present := state.Before(); present {
		mode := ""
		if before.PathMode != nil {
			mode = fmt.Sprintf("%d", *before.PathMode)
		}
		return strings.Join([]string{
			"before",
			fmt.Sprintf("%t", before.Existed),
			fmt.Sprintf("%t", before.PathExisted),
			fmt.Sprintf("%t", before.ParentExisted),
			before.Kind,
			before.ContentHash,
			before.BackupPath,
			before.LinkTarget,
			mode,
		}, "\x00")
	}
	expected, present := state.Expected()
	if !present {
		return "invalid"
	}
	mode := ""
	if expected.PathMode != nil {
		mode = fmt.Sprintf("%d", *expected.PathMode)
	}
	return strings.Join([]string{
		"expected",
		fmt.Sprintf("%t", expected.Existed),
		fmt.Sprintf("%t", expected.PathExisted),
		expected.Kind,
		expected.ContentHash,
		expected.LinkTarget,
		mode,
	}, "\x00")
}

// Scope returns the canonical portable scope.
func (demand RemovalDemand) Scope() target.Scope { return demand.scope }

// Destination returns the canonical rooted destination relation.
func (demand RemovalDemand) Destination() output.Destination { return demand.destination }

// States returns an owned copy of complete admitted removal states.
func (demand RemovalDemand) States() []RemovalState { return slices.Clone(demand.states) }

func (demand RemovalDemand) validate() error {
	_, err := NewRemovalDemand(demand.scope, demand.destination, demand.states)
	return err
}

// RemovalDemandSet is the deterministic complete set of logical-removal
// reachability facts for one executable operation. It is constructed once at
// the transition boundary and reused by journal capture and coverage checks.
type RemovalDemandSet struct {
	demands []RemovalDemand
}

// NewRemovalDemandSet constructs one duplicate-free canonical demand set.
func NewRemovalDemandSet(demands []RemovalDemand) (RemovalDemandSet, error) {
	copy := append([]RemovalDemand(nil), demands...)
	for index := range copy {
		if err := copy[index].validate(); err != nil {
			return RemovalDemandSet{}, fmt.Errorf("removal demand[%d]: %w", index, err)
		}
		for prior := 0; prior < index; prior++ {
			if copy[prior].scope == copy[index].scope && copy[prior].destination == copy[index].destination {
				return RemovalDemandSet{}, fmt.Errorf("removal demand set contains duplicate relation %q", copy[index].destination)
			}
		}
	}
	slices.SortFunc(copy, func(left, right RemovalDemand) int {
		if left.scope != right.scope {
			if left.scope < right.scope {
				return -1
			}
			return 1
		}
		return strings.Compare(left.destination.String(), right.destination.String())
	})
	return RemovalDemandSet{demands: copy}, nil
}

// Demands returns an owned copy of the canonical operation demand set.
func (set RemovalDemandSet) Demands() []RemovalDemand {
	return slices.Clone(set.demands)
}

// Len returns the number of rooted relations with reachable logical removal.
func (set RemovalDemandSet) Len() int { return len(set.demands) }

// Equal reports whether two sets carry the same sorted demand authority.
func (set RemovalDemandSet) Equal(other RemovalDemandSet) bool {
	if len(set.demands) != len(other.demands) {
		return false
	}
	for index := range set.demands {
		left, right := set.demands[index], other.demands[index]
		if left.scope != right.scope || left.destination != right.destination || len(left.states) != len(right.states) {
			return false
		}
		for _, state := range left.states {
			matched := false
			for _, candidate := range right.states {
				if state.equal(candidate) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
	}
	return true
}

// Validate checks the complete canonical demand set.
func (set RemovalDemandSet) Validate() error {
	_, err := NewRemovalDemandSet(set.demands)
	return err
}

// RemovalResidueEntryStatus classifies one exact residue entry independently
// of the namespace relation that contains it.
type RemovalResidueEntryStatus string

const (
	RemovalResidueEntryAbsent      RemovalResidueEntryStatus = "absent"
	RemovalResidueEntryPresent     RemovalResidueEntryStatus = "present"
	RemovalResidueEntryUnsupported RemovalResidueEntryStatus = "unsupported"
	RemovalResidueEntryUnavailable RemovalResidueEntryStatus = "unavailable"
)

// RemovalResidueEntryObservation is a bounded, no-follow semantic projection of
// one exact residue entry. It carries no filesystem capability.
type RemovalResidueEntryObservation struct {
	status      RemovalResidueEntryStatus
	kind        string
	contentHash string
	pathMode    *PermissionMode
	linkTarget  string
	detail      string
}

// RemovalNamespaceObservationStatus classifies the retained namespace
// independently from the exact residue entry below it.
type RemovalNamespaceObservationStatus string

const (
	RemovalNamespaceMatched     RemovalNamespaceObservationStatus = "matched"
	RemovalNamespaceChanged     RemovalNamespaceObservationStatus = "changed"
	RemovalNamespaceUnavailable RemovalNamespaceObservationStatus = "unavailable"
)

// RemovalNamespaceObservation is a bounded, no-follow projection of the
// relation-specific parent authority. It carries no capability or path
// selection policy.
type RemovalNamespaceObservation struct {
	status RemovalNamespaceObservationStatus
	detail string
}

// NewRemovalNamespaceObservation constructs one normalized namespace fact.
func NewRemovalNamespaceObservation(
	status RemovalNamespaceObservationStatus,
	detail string,
) (RemovalNamespaceObservation, error) {
	switch status {
	case RemovalNamespaceMatched:
		if detail != "" {
			return RemovalNamespaceObservation{}, fmt.Errorf("matched namespace observation must not contain detail")
		}
	case RemovalNamespaceChanged, RemovalNamespaceUnavailable:
		if strings.TrimSpace(detail) == "" {
			return RemovalNamespaceObservation{}, fmt.Errorf("%s namespace observation requires detail", status)
		}
	default:
		return RemovalNamespaceObservation{}, fmt.Errorf("unsupported namespace observation status %q", status)
	}
	return RemovalNamespaceObservation{status: status, detail: detail}, nil
}

// Status returns the orthogonal namespace observation status.
func (observation RemovalNamespaceObservation) Status() RemovalNamespaceObservationStatus {
	return observation.status
}

// Detail returns a public-safe namespace classification detail.
func (observation RemovalNamespaceObservation) Detail() string { return observation.detail }

// RemovalCleanupActionKind identifies the one physical operation needed to
// discharge a ready cleanup obligation.
type RemovalCleanupActionKind string

const (
	RemovalCleanupActionPromoteResidue  RemovalCleanupActionKind = "promote_residue"
	RemovalCleanupActionCleanupProgress RemovalCleanupActionKind = "cleanup_progress"
	RemovalCleanupActionConfirmAbsence  RemovalCleanupActionKind = "confirm_absence"
)

// RemovalCleanupReadiness is the physical-reconciliation state independent of
// semantic host/state/claim classification.
type RemovalCleanupReadiness string

const (
	RemovalCleanupPending    RemovalCleanupReadiness = "pending"
	RemovalCleanupReady      RemovalCleanupReadiness = "ready"
	RemovalCleanupBlocked    RemovalCleanupReadiness = "blocked"
	RemovalCleanupRetry      RemovalCleanupReadiness = "retry"
	RemovalCleanupDischarged RemovalCleanupReadiness = "discharged"
)

// RemovalCleanupReason is a stable classification reason for one cleanup
// obligation. It deliberately contains no machine-local path text.
type RemovalCleanupReason string

const (
	RemovalCleanupReasonNone                 RemovalCleanupReason = ""
	RemovalCleanupReasonNamespaceChanged     RemovalCleanupReason = "namespace_changed"
	RemovalCleanupReasonNamespaceUnavailable RemovalCleanupReason = "namespace_unavailable"
	RemovalCleanupReasonResidueMismatch      RemovalCleanupReason = "residue_mismatch"
	RemovalCleanupReasonResidueUnsupported   RemovalCleanupReason = "residue_unsupported"
	RemovalCleanupReasonResidueUnavailable   RemovalCleanupReason = "residue_unavailable"
	RemovalCleanupReasonCleanupCollision     RemovalCleanupReason = "cleanup_stage_collision"
	RemovalCleanupReasonCleanupMismatch      RemovalCleanupReason = "cleanup_stage_mismatch"
	RemovalCleanupReasonCleanupUnsupported   RemovalCleanupReason = "cleanup_stage_unsupported"
	RemovalCleanupReasonCleanupUnavailable   RemovalCleanupReason = "cleanup_stage_unavailable"
)

// RemovalCleanupObligation is the immutable operation-scoped obligation
// derived from one RemovalIntent. It contains no filesystem capability and
// does not decide the relation's namespace or entry observations.
type RemovalCleanupObligation struct {
	scope       target.Scope
	destination output.Destination
	names       mutationfs.LogicalRemovalNames
	action      RemovalCleanupActionKind
	readiness   RemovalCleanupReadiness
	reason      RemovalCleanupReason
	detail      string
}

// NewPendingRemovalCleanupObligation creates the obligation established by a
// complete intent before fresh residue reconciliation.
func NewPendingRemovalCleanupObligation(intent RemovalIntent) (RemovalCleanupObligation, error) {
	if err := intent.Validate(); err != nil {
		return RemovalCleanupObligation{}, err
	}
	return RemovalCleanupObligation{
		scope:       intent.Scope(),
		destination: intent.Destination(),
		names:       intent.Namespace().Names(),
		readiness:   RemovalCleanupPending,
	}, nil
}

// Scope returns the obligation relation scope.
func (obligation RemovalCleanupObligation) Scope() target.Scope { return obligation.scope }

// Destination returns the obligation's canonical rooted relation.
func (obligation RemovalCleanupObligation) Destination() output.Destination {
	return obligation.destination
}

// Names returns both exact namespace slots carried by the obligation.
func (obligation RemovalCleanupObligation) Names() mutationfs.LogicalRemovalNames {
	return obligation.names
}

// Action returns the exact ready physical action, when one is selected.
func (obligation RemovalCleanupObligation) Action() RemovalCleanupActionKind {
	return obligation.action
}

// Readiness returns the physical reconciliation status.
func (obligation RemovalCleanupObligation) Readiness() RemovalCleanupReadiness {
	return obligation.readiness
}

// Reason returns a stable typed classification reason.
func (obligation RemovalCleanupObligation) Reason() RemovalCleanupReason { return obligation.reason }

// Detail returns a public-safe classification detail.
func (obligation RemovalCleanupObligation) Detail() string { return obligation.detail }

// Discharge records that the selected exact action completed its storage
// protocol. Only a ready obligation can be discharged.
func (obligation RemovalCleanupObligation) Discharge() (RemovalCleanupObligation, error) {
	if obligation.readiness != RemovalCleanupReady {
		return RemovalCleanupObligation{}, fmt.Errorf("removal cleanup obligation is not ready: %s", obligation.readiness)
	}
	obligation.readiness = RemovalCleanupDischarged
	return obligation, nil
}

// Equal compares the complete immutable obligation/result value.
func (obligation RemovalCleanupObligation) Equal(other RemovalCleanupObligation) bool {
	return obligation.scope == other.scope &&
		obligation.destination == other.destination &&
		obligation.names.Equal(other.names) &&
		obligation.action == other.action &&
		obligation.readiness == other.readiness &&
		obligation.reason == other.reason &&
		obligation.detail == other.detail
}

// SameBasis reports whether two obligations refer to the same immutable
// operation relation, independently of their live readiness.
func (obligation RemovalCleanupObligation) SameBasis(other RemovalCleanupObligation) bool {
	return obligation.scope == other.scope &&
		obligation.destination == other.destination &&
		obligation.names.Equal(other.names)
}

// NewPendingRemovalCleanupObligations derives the complete ordered obligation
// set from complete authority. It never narrows to a selected recovery subset.
func NewPendingRemovalCleanupObligations(
	intents []RemovalIntent,
) ([]RemovalCleanupObligation, error) {
	obligations := make([]RemovalCleanupObligation, 0, len(intents))
	for index, intent := range intents {
		obligation, err := NewPendingRemovalCleanupObligation(intent)
		if err != nil {
			return nil, fmt.Errorf("removal intent[%d] obligation: %w", index, err)
		}
		obligations = append(obligations, obligation)
	}
	return obligations, nil
}

// NewRemovalResidueEntryObservation constructs one normalized entry observation.
func NewRemovalResidueEntryObservation(
	status RemovalResidueEntryStatus,
	kind string,
	contentHash string,
	pathMode *PermissionMode,
	linkTarget string,
	detail string,
) (RemovalResidueEntryObservation, error) {
	switch status {
	case RemovalResidueEntryAbsent:
		if kind != "" || contentHash != "" || pathMode != nil || linkTarget != "" {
			return RemovalResidueEntryObservation{}, fmt.Errorf("absent residue observation carries entry facts")
		}
	case RemovalResidueEntryPresent:
		if err := validateRemovalExistingFacts(kind, contentHash, "", linkTarget, pathMode); err != nil {
			return RemovalResidueEntryObservation{}, err
		}
	case RemovalResidueEntryUnsupported, RemovalResidueEntryUnavailable:
		if detail == "" {
			return RemovalResidueEntryObservation{}, fmt.Errorf("%s residue observation requires detail", status)
		}
	default:
		return RemovalResidueEntryObservation{}, fmt.Errorf("unsupported residue entry status %q", status)
	}
	return RemovalResidueEntryObservation{
		status: status, kind: kind, contentHash: contentHash,
		pathMode: clonePermissionMode(pathMode), linkTarget: linkTarget, detail: detail,
	}, nil
}

// RemovalResidueObservation keeps the pre-cleanup residue and durable cleanup
// stage separate. Presence under the cleanup-stage name is progress evidence;
// it is never reinterpreted as an unvalidated original residue.
type RemovalResidueObservation struct {
	residue RemovalResidueEntryObservation
	cleanup RemovalResidueEntryObservation
}

// NewRemovalResidueObservation constructs one complete two-slot observation.
func NewRemovalResidueObservation(
	residue RemovalResidueEntryObservation,
	cleanup RemovalResidueEntryObservation,
) RemovalResidueObservation {
	return RemovalResidueObservation{residue: residue, cleanup: cleanup}
}

// Residue returns the exact pre-cleanup slot observation.
func (observation RemovalResidueObservation) Residue() RemovalResidueEntryObservation {
	return observation.residue
}

// Cleanup returns the exact durable cleanup-stage slot observation.
func (observation RemovalResidueObservation) Cleanup() RemovalResidueEntryObservation {
	return observation.cleanup
}

// Status returns the orthogonal entry observation status.
func (observation RemovalResidueEntryObservation) Status() RemovalResidueEntryStatus {
	return observation.status
}

// Kind returns the admitted physical kind when the entry is present.
func (observation RemovalResidueEntryObservation) Kind() string { return observation.kind }

// ContentHash returns the bounded content or tree hash when present.
func (observation RemovalResidueEntryObservation) ContentHash() string {
	return observation.contentHash
}

// PathMode returns the observed regular-file permission mode when present.
func (observation RemovalResidueEntryObservation) PathMode() *PermissionMode {
	return clonePermissionMode(observation.pathMode)
}

// LinkTarget returns the observed symlink target when present.
func (observation RemovalResidueEntryObservation) LinkTarget() string {
	return observation.linkTarget
}

// Detail returns a public-safe classification detail.
func (observation RemovalResidueEntryObservation) Detail() string { return observation.detail }

// AdmitsEntry reports whether the entry facts match one authorized removable
// whole-path state. Absent is handled by the storage durability protocol.
func (intent RemovalIntent) AdmitsEntry(observation RemovalResidueEntryObservation) bool {
	if observation.status != RemovalResidueEntryPresent {
		return false
	}
	for _, state := range intent.states {
		if removalStateAdmitsEntry(state, observation) {
			return true
		}
	}
	return false
}

// AdmitsCleanupProgress reports whether a cleanup-stage entry remains within
// an authorized removable state. Recursive cleanup may change a directory's
// tree hash, but regular files and symlinks remain exact until atomic unlink.
func (intent RemovalIntent) AdmitsCleanupProgress(observation RemovalResidueEntryObservation) bool {
	if observation.status != RemovalResidueEntryPresent {
		return false
	}
	if observation.kind != PathKindDirectory {
		return intent.AdmitsEntry(observation)
	}
	for _, state := range intent.states {
		if before, present := state.Before(); present && before.Existed && before.Kind == observation.kind {
			return true
		}
		if expected, present := state.Expected(); present && expected.Existed && expected.Kind == observation.kind {
			return true
		}
	}
	return false
}

func removalStateAdmitsEntry(state RemovalState, observation RemovalResidueEntryObservation) bool {
	kind, hash, mode, linkTarget := observation.kind, observation.contentHash, observation.pathMode, observation.linkTarget
	if before, present := state.Before(); present {
		return before.Existed && before.Kind == kind && before.ContentHash == hash &&
			permissionModesEqual(before.PathMode, mode) && before.LinkTarget == linkTarget
	}
	expected, present := state.Expected()
	return present && expected.Existed && expected.Kind == kind && expected.ContentHash == hash &&
		permissionModesEqual(expected.PathMode, mode) && expected.LinkTarget == linkTarget
}

// RemovalIntent is immutable durable authority for one operation-scoped rooted
// removal relation.
type RemovalIntent struct {
	scope       target.Scope
	destination output.Destination
	namespace   RemovalNamespaceAuthority
	states      []RemovalState
}

// NewRemovalIntent binds a demand to exact namespace authority and removal names.
func NewRemovalIntent(demand RemovalDemand, namespace RemovalNamespaceAuthority) (RemovalIntent, error) {
	if err := demand.validate(); err != nil {
		return RemovalIntent{}, err
	}
	if err := namespace.validate(); err != nil {
		return RemovalIntent{}, fmt.Errorf("removal intent namespace: %w", err)
	}
	return RemovalIntent{
		scope:       demand.scope,
		destination: demand.destination,
		namespace:   namespace,
		states:      demand.States(),
	}, nil
}

// Demand returns the transition-derived demand carried by this intent.
func (intent RemovalIntent) Demand() (RemovalDemand, error) {
	return NewRemovalDemand(intent.scope, intent.destination, intent.states)
}

// Scope returns the intent relation scope.
func (intent RemovalIntent) Scope() target.Scope { return intent.scope }

// Destination returns the intent relation destination.
func (intent RemovalIntent) Destination() output.Destination { return intent.destination }

// Namespace returns immutable namespace authority.
func (intent RemovalIntent) Namespace() RemovalNamespaceAuthority { return intent.namespace }

// States returns all complete removable whole-path states.
func (intent RemovalIntent) States() []RemovalState { return slices.Clone(intent.states) }

// AdmitsState reports whether a fresh whole-path state is one of the exact
// states authorized by this intent.
func (intent RemovalIntent) AdmitsState(state RemovalState) bool {
	for _, candidate := range intent.states {
		if candidate.equal(state) {
			return true
		}
	}
	return false
}

// AssessCleanup combines orthogonal namespace and entry observations into a
// typed cleanup obligation. It never turns an exact residue into permission
// when the visible recovery plan is semantically blocked; that gate remains a
// separate Plan/execute decision.
func (intent RemovalIntent) AssessCleanup(
	namespace RemovalNamespaceObservation,
	entries RemovalResidueObservation,
) (RemovalCleanupObligation, error) {
	obligation, err := NewPendingRemovalCleanupObligation(intent)
	if err != nil {
		return RemovalCleanupObligation{}, err
	}
	switch namespace.Status() {
	case RemovalNamespaceChanged:
		obligation.readiness = RemovalCleanupBlocked
		obligation.reason = RemovalCleanupReasonNamespaceChanged
		obligation.detail = namespace.Detail()
		return obligation, nil
	case RemovalNamespaceUnavailable:
		obligation.readiness = RemovalCleanupRetry
		obligation.reason = RemovalCleanupReasonNamespaceUnavailable
		obligation.detail = namespace.Detail()
		return obligation, nil
	case RemovalNamespaceMatched:
	default:
		return RemovalCleanupObligation{}, fmt.Errorf("unsupported namespace observation status %q", namespace.Status())
	}

	residueEntry := entries.Residue()
	cleanupEntry := entries.Cleanup()
	if result, decided := assessUnavailableOrUnsupportedCleanupSlot(obligation, cleanupEntry); decided {
		return result, nil
	}
	if result, decided := assessUnavailableOrUnsupportedResidueSlot(obligation, residueEntry); decided {
		return result, nil
	}

	switch {
	case residueEntry.Status() == RemovalResidueEntryAbsent && cleanupEntry.Status() == RemovalResidueEntryAbsent:
		obligation.readiness = RemovalCleanupReady
		obligation.action = RemovalCleanupActionConfirmAbsence
	case residueEntry.Status() == RemovalResidueEntryPresent && cleanupEntry.Status() == RemovalResidueEntryAbsent:
		if !intent.AdmitsEntry(residueEntry) {
			obligation.readiness = RemovalCleanupBlocked
			obligation.reason = RemovalCleanupReasonResidueMismatch
			obligation.detail = "residue does not match an authorized whole-path state"
			return obligation, nil
		}
		obligation.readiness = RemovalCleanupReady
		obligation.action = RemovalCleanupActionPromoteResidue
	case residueEntry.Status() == RemovalResidueEntryAbsent && cleanupEntry.Status() == RemovalResidueEntryPresent:
		if !intent.AdmitsCleanupProgress(cleanupEntry) {
			obligation.readiness = RemovalCleanupBlocked
			obligation.reason = RemovalCleanupReasonCleanupMismatch
			obligation.detail = "cleanup-stage entry does not match authorized cleanup progress"
			return obligation, nil
		}
		obligation.readiness = RemovalCleanupReady
		obligation.action = RemovalCleanupActionCleanupProgress
	case residueEntry.Status() == RemovalResidueEntryPresent && cleanupEntry.Status() == RemovalResidueEntryPresent:
		obligation.readiness = RemovalCleanupBlocked
		obligation.reason = RemovalCleanupReasonCleanupCollision
		obligation.detail = "residue and cleanup-stage names are both occupied"
	default:
		return RemovalCleanupObligation{}, fmt.Errorf(
			"unsupported removal slot state %q/%q",
			residueEntry.Status(),
			cleanupEntry.Status(),
		)
	}
	return obligation, nil
}

func assessUnavailableOrUnsupportedCleanupSlot(
	obligation RemovalCleanupObligation,
	entry RemovalResidueEntryObservation,
) (RemovalCleanupObligation, bool) {
	switch entry.Status() {
	case RemovalResidueEntryUnsupported:
		obligation.readiness = RemovalCleanupBlocked
		obligation.reason = RemovalCleanupReasonCleanupUnsupported
		obligation.detail = entry.Detail()
		return obligation, true
	case RemovalResidueEntryUnavailable:
		obligation.readiness = RemovalCleanupRetry
		obligation.reason = RemovalCleanupReasonCleanupUnavailable
		obligation.detail = entry.Detail()
		return obligation, true
	default:
		return RemovalCleanupObligation{}, false
	}
}

func assessUnavailableOrUnsupportedResidueSlot(
	obligation RemovalCleanupObligation,
	entry RemovalResidueEntryObservation,
) (RemovalCleanupObligation, bool) {
	switch entry.Status() {
	case RemovalResidueEntryUnsupported:
		obligation.readiness = RemovalCleanupBlocked
		obligation.reason = RemovalCleanupReasonResidueUnsupported
		obligation.detail = entry.Detail()
		return obligation, true
	case RemovalResidueEntryUnavailable:
		obligation.readiness = RemovalCleanupRetry
		obligation.reason = RemovalCleanupReasonResidueUnavailable
		obligation.detail = entry.Detail()
		return obligation, true
	default:
		return RemovalCleanupObligation{}, false
	}
}

// Validate checks the complete immutable intent contract.
func (intent RemovalIntent) Validate() error { return intent.validate() }

// Equal compares complete immutable removal authority.
func (intent RemovalIntent) Equal(other RemovalIntent) bool { return intent.equal(other) }

func (intent RemovalIntent) validate() error {
	demand, err := NewRemovalDemand(intent.scope, intent.destination, intent.states)
	if err != nil {
		return err
	}
	_, err = NewRemovalIntent(demand, intent.namespace)
	return err
}

func (intent RemovalIntent) equal(other RemovalIntent) bool {
	if intent.scope != other.scope || intent.destination != other.destination || !intent.namespace.equal(other.namespace) || len(intent.states) != len(other.states) {
		return false
	}
	for _, state := range intent.states {
		matched := false
		for _, candidate := range other.states {
			if state.equal(candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
