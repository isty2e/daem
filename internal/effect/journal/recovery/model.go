package recovery

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

// ManifestRootProvenance is the wire-neutral physical root identity recorded by
// every journal. It never grants current filesystem authority.
type ManifestRootProvenance struct {
	physicalRoot      string
	objectFingerprint string
	mountFingerprint  string
}

// NewManifestRootProvenance constructs validated canonical root provenance from
// an already validated persistence or retained-authority boundary.
func NewManifestRootProvenance(physicalRoot string, objectFingerprint string, mountFingerprint string) (ManifestRootProvenance, error) {
	provenance := ManifestRootProvenance{
		physicalRoot:      physicalRoot,
		objectFingerprint: objectFingerprint,
		mountFingerprint:  mountFingerprint,
	}
	if err := provenance.validate(); err != nil {
		return ManifestRootProvenance{}, fmt.Errorf("manifest root provenance requires physical root, object fingerprint, and mount fingerprint")
	}
	return provenance, nil
}

func (provenance ManifestRootProvenance) validate() error {
	if provenance.physicalRoot == "" || provenance.objectFingerprint == "" || provenance.mountFingerprint == "" {
		return fmt.Errorf("manifest root provenance requires physical root, object fingerprint, and mount fingerprint")
	}
	return nil
}

// Equal reports whether two provenance facts name the same physical root.
func (provenance ManifestRootProvenance) Equal(other ManifestRootProvenance) bool {
	return provenance == other
}

// PhysicalRoot returns the canonical physical root spelling in the evidence.
func (provenance ManifestRootProvenance) PhysicalRoot() string { return provenance.physicalRoot }

// ObjectFingerprint returns opaque durable object evidence.
func (provenance ManifestRootProvenance) ObjectFingerprint() string {
	return provenance.objectFingerprint
}

// MountFingerprint returns opaque durable mount evidence.
func (provenance ManifestRootProvenance) MountFingerprint() string {
	return provenance.mountFingerprint
}

// Authority is the complete canonical durable journal authority. Selection
// may narrow observations and actions but never this value.
type Authority struct {
	operationID        string
	operationDir       string
	entries            []Entry
	removalIntents     []RemovalIntent
	statefileBefore    durable.Snapshot
	statefileAfter     durable.Snapshot
	claimTransitions   []ownershipmutation.ClaimTransition
	provisionalIntents []outputownership.ProvisionalAcquireIntent
	manifestProvenance ManifestRootProvenance
	fingerprint        string
}

// NewAuthority constructs complete immutable authority from one fully
// validated journal.
func NewAuthority(
	operationID string,
	operationDir string,
	entries []Entry,
	statefileBefore durable.Snapshot,
	statefileAfter durable.Snapshot,
	claimTransitions []ownershipmutation.ClaimTransition,
	provisionalIntents []outputownership.ProvisionalAcquireIntent,
	manifestProvenance ManifestRootProvenance,
	fingerprint string,
	removalIntents []RemovalIntent,
) (Authority, error) {
	if operationID == "" || operationDir == "" || fingerprint == "" {
		return Authority{}, fmt.Errorf("recovery authority requires operation identity, directory, and fingerprint")
	}
	if entries == nil {
		return Authority{}, fmt.Errorf("recovery authority entries are required")
	}
	for index, entry := range entries {
		if err := entry.validate(); err != nil {
			return Authority{}, fmt.Errorf("recovery authority entries[%d]: %w", index, err)
		}
	}
	for index, transition := range claimTransitions {
		if err := transition.Validate(); err != nil {
			return Authority{}, fmt.Errorf("recovery authority claim transitions[%d]: %w", index, err)
		}
	}
	for index, intent := range provisionalIntents {
		if err := intent.Validate(); err != nil {
			return Authority{}, fmt.Errorf("recovery authority provisional intents[%d]: %w", index, err)
		}
	}
	removalIntents = append([]RemovalIntent(nil), removalIntents...)
	removalNames := make(map[string]struct{}, len(removalIntents)*2)
	for index, intent := range removalIntents {
		if err := intent.validate(); err != nil {
			return Authority{}, fmt.Errorf("recovery authority removal intents[%d]: %w", index, err)
		}
		for prior := range index {
			if removalIntents[prior].scope == intent.scope && removalIntents[prior].destination == intent.destination {
				return Authority{}, fmt.Errorf("recovery authority removal intents contain duplicate relation %q", intent.destination)
			}
		}
		for _, name := range []string{intent.namespace.names.Residue(), intent.namespace.names.Cleanup()} {
			if _, duplicate := removalNames[name]; duplicate {
				return Authority{}, fmt.Errorf("recovery authority removal intents contain duplicate namespace name %q", name)
			}
			removalNames[name] = struct{}{}
		}
	}
	if err := manifestProvenance.validate(); err != nil {
		return Authority{}, fmt.Errorf("recovery authority manifest root: %w", err)
	}
	authority := Authority{
		operationID:        operationID,
		operationDir:       operationDir,
		entries:            cloneEntries(entries),
		removalIntents:     append([]RemovalIntent(nil), removalIntents...),
		statefileBefore:    statefileBefore,
		statefileAfter:     statefileAfter,
		claimTransitions:   append([]ownershipmutation.ClaimTransition(nil), claimTransitions...),
		provisionalIntents: append([]outputownership.ProvisionalAcquireIntent(nil), provisionalIntents...),
		manifestProvenance: manifestProvenance,
		fingerprint:        fingerprint,
	}
	return authority, nil
}

func cloneEntries(entries []Entry) []Entry {
	cloned := make([]Entry, len(entries))
	for index, entry := range entries {
		cloned[index] = entry
		cloned[index].consumerTargets = append([]target.Target(nil), entry.consumerTargets...)
		cloned[index].before = cloneBeforePathState(entry.before)
		cloned[index].expectedAfter = entry.expectedAfter.Clone()
		if entry.aggregateContract != nil {
			contract := entry.aggregateContract.Clone()
			cloned[index].aggregateContract = &contract
		}
	}
	return cloned
}

// Selection is an opaque, validated subset of one authority.
type Selection struct {
	indexes              []int
	operationID          string
	operationDir         string
	authorityFingerprint string
	entryCount           int
	initialized          bool
}

// NewSelection validates selected entry ordinals against complete authority.
func NewSelection(authority Authority, indexes []int) (Selection, error) {
	if authority.operationID == "" || authority.operationDir == "" ||
		authority.fingerprint == "" || authority.entries == nil {
		return Selection{}, fmt.Errorf("recovery selection requires initialized authority")
	}
	if indexes == nil {
		indexes = make([]int, len(authority.entries))
		for index := range authority.entries {
			indexes[index] = index
		}
	}
	seen := make(map[int]struct{}, len(indexes))
	for _, index := range indexes {
		if index < 0 || index >= len(authority.entries) {
			return Selection{}, fmt.Errorf("recovery selection index %d is outside authority", index)
		}
		if _, duplicate := seen[index]; duplicate {
			return Selection{}, fmt.Errorf("duplicate recovery selection index %d", index)
		}
		seen[index] = struct{}{}
	}
	return Selection{
		indexes:              append([]int(nil), indexes...),
		operationID:          authority.operationID,
		operationDir:         authority.operationDir,
		authorityFingerprint: authority.fingerprint,
		entryCount:           len(authority.entries),
		initialized:          true,
	}, nil
}

func (selection Selection) validate(authority Authority) error {
	if !selection.initialized {
		return fmt.Errorf("recovery selection is uninitialized")
	}
	if selection.operationID != authority.operationID ||
		selection.operationDir != authority.operationDir ||
		selection.authorityFingerprint != authority.fingerprint ||
		selection.entryCount != len(authority.entries) {
		return fmt.Errorf("recovery selection belongs to different authority")
	}
	return nil
}

// PathEvidence is one fresh path observation normalized by the journal
// boundary. Errors are facts for classification, not boundary decisions.
type PathEvidence struct {
	Path          string
	ContentPath   string
	Exists        bool
	PathExisted   bool
	PathMode      *PermissionMode
	Kind          string
	ContentHash   string
	LinkTarget    string
	BlockedReason string
	BlockedDetail string
	Error         string
}

// BackupEvidence is one fresh backup observation.
type BackupEvidence struct {
	BackupPath  string
	Exists      bool
	Kind        string
	ContentHash string
	Error       string
}

// Classification reports whether active journal authority is clean,
// recoverable, or blocked by fresh evidence.
type Classification string

const (
	ClassificationCleanBefore   Classification = "clean_before"
	ClassificationCleanAfter    Classification = "clean_after"
	ClassificationNeedsRollback Classification = "needs_rollback"
	ClassificationNeedsFinalize Classification = "needs_finalize"
	ClassificationBlocked       Classification = "blocked"
)

// ActionKind classifies one pure recovery action.
type ActionKind string

const (
	ActionKindCleanup        ActionKind = "cleanup"
	ActionKindRestoreWrite   ActionKind = "restore_write"
	ActionKindRestoreDelete  ActionKind = "restore_delete"
	ActionKindNoOp           ActionKind = "noop"
	ActionKindError          ActionKind = "error"
	ActionKindFinalizeClaims ActionKind = "finalize_claims"
)

// Action is an effect-ready recovery fact derived from complete authority and
// fresh evidence.
type Action struct {
	Kind                ActionKind
	Reason              string
	subject             topology.SubjectID
	Target              target.Target
	ConsumerTargets     []target.Target
	Scope               target.Scope
	Destination         string
	ContentPath         string
	ContentKind         realization.PathProjectionContentKind
	BackupPath          string
	BackupHash          string
	BackupKind          string
	BeforePathMode      *PermissionMode
	BeforePathExisted   bool
	BeforeParentExisted bool
	ExpectedAfter       ExpectedPathState
	AggregateContract   *aggregate.ProjectionContract
	Detail              string
}

// SubjectID returns canonical subject identity for subject-owned actions.
func (action Action) SubjectID() (topology.SubjectID, bool) {
	return action.subject, !action.subject.IsZero()
}

// Plan is a pure classification over complete durable authority and fresh
// evidence.
type Plan struct {
	authority          Authority
	classification     Classification
	actions            []Action
	guardedActions     []Action
	removalObligations []RemovalCleanupObligation
}

// Clone returns a disclosure-safe plan copy.
func (plan Plan) Clone() Plan {
	plan.actions = cloneActions(plan.actions)
	plan.guardedActions = cloneActions(plan.guardedActions)
	plan.removalObligations = slices.Clone(plan.removalObligations)
	return plan
}

// ClaimTransitions returns validated operation-scoped ownership transitions.
func (plan Plan) ClaimTransitions() []ownershipmutation.ClaimTransition {
	return append([]ownershipmutation.ClaimTransition(nil), plan.authority.claimTransitions...)
}

// ProvisionalAcquireIntents returns operation-scoped acquisition intents that
// have not yet been promoted to exact durable claims.
func (plan Plan) ProvisionalAcquireIntents() []outputownership.ProvisionalAcquireIntent {
	return append([]outputownership.ProvisionalAcquireIntent(nil), plan.authority.provisionalIntents...)
}

// RemovalIntents returns complete operation-scoped cleanup authority. Selection
// never narrows this set.
func (plan Plan) RemovalIntents() []RemovalIntent {
	return append([]RemovalIntent(nil), plan.authority.removalIntents...)
}

// RemovalIntentFor returns the exact intent for one rooted destination relation.
func (plan Plan) RemovalIntentFor(scope target.Scope, destination output.Destination) (RemovalIntent, bool) {
	for _, intent := range plan.authority.removalIntents {
		if intent.scope == scope && intent.destination == destination {
			return intent, true
		}
	}
	return RemovalIntent{}, false
}

// RemovalCleanupObligations returns the complete operation-scoped cleanup
// basis. Selection never narrows this set; freshly reconciled results are
// checked against it by RetirementReady.
func (plan Plan) RemovalCleanupObligations() []RemovalCleanupObligation {
	return slices.Clone(plan.removalObligations)
}

// RetirementReady reports whether visible convergence and every complete
// authority cleanup obligation have both been established. The caller must
// supply the discharged results from the effect-time retirement gate; a plan
// cannot self-authorize retirement from semantic clean classification alone.
func (plan Plan) RetirementReady(discharged []RemovalCleanupObligation) bool {
	if plan.classification != ClassificationCleanBefore && plan.classification != ClassificationCleanAfter {
		return false
	}
	if len(discharged) != len(plan.removalObligations) {
		return false
	}
	for _, expected := range plan.removalObligations {
		matched := false
		for _, actual := range discharged {
			if actual.Readiness() == RemovalCleanupDischarged && expected.SameBasis(actual) {
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

func (plan Plan) Blocked() bool                     { return plan.classification == ClassificationBlocked }
func (plan Plan) Classification() Classification    { return plan.classification }
func (plan Plan) OperationID() string               { return plan.authority.operationID }
func (plan Plan) OperationDir() string              { return plan.authority.operationDir }
func (plan Plan) Actions() []Action                 { return cloneActions(plan.actions) }
func (plan Plan) GuardedActions() []Action          { return cloneActions(plan.guardedActions) }
func (plan Plan) StatefileBefore() durable.Snapshot { return plan.authority.statefileBefore }

// JournalAuthorityFingerprint returns the opaque digest of complete canonical
// journal authority, including unselected entries.
func (plan Plan) JournalAuthorityFingerprint() (string, error) {
	if plan.authority.fingerprint == "" {
		return "", fmt.Errorf("recovery journal authority fingerprint is unavailable")
	}
	return plan.authority.fingerprint, nil
}

func (plan Plan) HasErrors() bool {
	if plan.Blocked() {
		return true
	}
	for _, action := range plan.actions {
		if action.Kind == ActionKindError {
			return true
		}
	}
	return false
}

// MatchManifestRootProvenance compares fresh retained-root facts without
// accepting or granting a capability.
func (plan Plan) MatchManifestRootProvenance(actual ManifestRootProvenance) error {
	if !plan.authority.manifestProvenance.Equal(actual) {
		return fmt.Errorf("manifest root provenance changed")
	}
	return nil
}

// SameExecutionAuthority reports whether two plans authorize identical
// effects after final re-observation.
func (plan Plan) SameExecutionAuthority(other Plan) bool {
	if plan.authority.operationID != other.authority.operationID ||
		plan.authority.operationDir != other.authority.operationDir ||
		plan.authority.fingerprint != other.authority.fingerprint ||
		plan.classification != other.classification ||
		!plan.authority.statefileBefore.Equal(other.authority.statefileBefore) ||
		!plan.authority.statefileAfter.Equal(other.authority.statefileAfter) ||
		!plan.authority.manifestProvenance.Equal(other.authority.manifestProvenance) ||
		len(plan.actions) != len(other.actions) ||
		len(plan.guardedActions) != len(other.guardedActions) ||
		len(plan.authority.claimTransitions) != len(other.authority.claimTransitions) ||
		len(plan.authority.provisionalIntents) != len(other.authority.provisionalIntents) ||
		len(plan.authority.removalIntents) != len(other.authority.removalIntents) ||
		len(plan.removalObligations) != len(other.removalObligations) {
		return false
	}
	for index := range plan.actions {
		if !plan.actions[index].sameExecutionAuthority(other.actions[index]) {
			return false
		}
	}
	for index := range plan.guardedActions {
		if !plan.guardedActions[index].sameExecutionAuthority(other.guardedActions[index]) {
			return false
		}
	}
	for index := range plan.authority.claimTransitions {
		if !plan.authority.claimTransitions[index].Equal(other.authority.claimTransitions[index]) {
			return false
		}
	}
	for index := range plan.authority.provisionalIntents {
		if !plan.authority.provisionalIntents[index].Equal(other.authority.provisionalIntents[index]) {
			return false
		}
	}
	for index := range plan.authority.removalIntents {
		if !plan.authority.removalIntents[index].equal(other.authority.removalIntents[index]) {
			return false
		}
	}
	for index := range plan.removalObligations {
		if !plan.removalObligations[index].Equal(other.removalObligations[index]) {
			return false
		}
	}
	return true
}

func cloneActions(actions []Action) []Action {
	if actions == nil {
		return nil
	}
	cloned := make([]Action, len(actions))
	for index, action := range actions {
		cloned[index] = action
		cloned[index].ConsumerTargets = append([]target.Target(nil), action.ConsumerTargets...)
		cloned[index].BeforePathMode = clonePermissionMode(action.BeforePathMode)
		cloned[index].ExpectedAfter = action.ExpectedAfter.Clone()
		if action.AggregateContract != nil {
			contract := action.AggregateContract.Clone()
			cloned[index].AggregateContract = &contract
		}
	}
	return cloned
}

func (action Action) sameExecutionAuthority(other Action) bool {
	return action.Kind == other.Kind && action.Reason == other.Reason &&
		action.subject == other.subject && action.Target == other.Target &&
		slices.Equal(action.ConsumerTargets, other.ConsumerTargets) &&
		action.Scope == other.Scope && action.Destination == other.Destination &&
		action.ContentPath == other.ContentPath && action.ContentKind == other.ContentKind &&
		action.BackupPath == other.BackupPath && action.BackupHash == other.BackupHash &&
		action.BackupKind == other.BackupKind && action.ExpectedAfter.Equal(other.ExpectedAfter) &&
		aggregateContractsEqual(action.AggregateContract, other.AggregateContract) &&
		permissionModesEqual(action.BeforePathMode, other.BeforePathMode) &&
		action.BeforePathExisted == other.BeforePathExisted &&
		action.BeforeParentExisted == other.BeforeParentExisted && action.Detail == other.Detail
}

func aggregateContractsEqual(left *aggregate.ProjectionContract, right *aggregate.ProjectionContract) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
