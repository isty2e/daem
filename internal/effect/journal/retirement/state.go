package retirement

import (
	"fmt"
	"slices"
	"strings"
)

// LayoutEvidence is the pre-canonical inventory for one recovery root.
// Unrelated hidden entries are intentionally omitted.
type LayoutEvidence struct {
	active      []Identity
	controls    []Control
	residues    []Residue
	garbage     []Garbage
	blockers    []Blocker
	initialized bool
}

// NewLayoutEvidence records one complete recovery-root observation. Slice
// ownership transfers by copy so later boundary mutation cannot change a
// classification.
func NewLayoutEvidence(
	active []Identity,
	controls []Control,
	residues []Residue,
	garbage []Garbage,
	blockers []Blocker,
) LayoutEvidence {
	return LayoutEvidence{
		active:      append([]Identity(nil), active...),
		controls:    append([]Control(nil), controls...),
		residues:    append([]Residue(nil), residues...),
		garbage:     append([]Garbage(nil), garbage...),
		blockers:    append([]Blocker(nil), blockers...),
		initialized: true,
	}
}

// State is the closed journal-retirement layout state.
type State string

const (
	StateClean      State = "clean"
	StateActive     State = "active"
	StatePrepared   State = "prepared"
	StateRetained   State = "retained"
	StateFinalizing State = "finalizing"
	StateFinalized  State = "finalized"
	StateBlocked    State = "blocked"
)

// Decision is one canonical layout classification.
type Decision struct {
	state   State
	detail  string
	cleanup *CleanupPlan
}

// Classify applies the closed journal-retirement state table.
func Classify(evidence LayoutEvidence) Decision {
	if !evidence.initialized {
		return blocked("journal retirement inventory is incomplete")
	}
	if detail := firstBlockerDetail(evidence.blockers); detail != "" {
		return blocked(detail)
	}
	if len(evidence.active) > 1 {
		return blocked("multiple active recovery journals found")
	}
	if len(evidence.controls) > 1 {
		return blocked("multiple journal retirement controls found")
	}

	seenArtifacts := make(map[string]struct{}, len(evidence.residues)+len(evidence.garbage))
	for _, residue := range evidence.residues {
		if residue.name.kind != NameResidue || !residue.journalIdentity.valid() ||
			!residue.name.BelongsTo(residue.journalIdentity) {
			return blocked("retirement inventory contains an uninitialized residue")
		}
		if _, duplicate := seenArtifacts[residue.name.value]; duplicate {
			return blocked(fmt.Sprintf("duplicate retirement artifact %q", residue.name.value))
		}
		seenArtifacts[residue.name.value] = struct{}{}
	}
	for _, garbage := range evidence.garbage {
		if garbage.name.kind != NameGC || !garbage.name.valid() {
			return blocked("retirement inventory contains uninitialized GC residue")
		}
		if _, duplicate := seenArtifacts[garbage.name.value]; duplicate {
			return blocked(fmt.Sprintf("duplicate retirement artifact %q", garbage.name.value))
		}
		seenArtifacts[garbage.name.value] = struct{}{}
	}
	if len(evidence.residues) > 1 {
		return blocked("multiple journal retirement residues found")
	}

	var active Identity
	if len(evidence.active) == 1 {
		active = evidence.active[0]
		if !active.valid() {
			return blocked("active recovery identity is uninitialized")
		}
	}
	var control Control
	if len(evidence.controls) == 1 {
		control = evidence.controls[0]
		if !control.record.valid() {
			return blocked("journal retirement control is uninitialized")
		}
	}
	if matchingGC(active, control, evidence.residues, evidence.garbage) {
		return blocked("current journal retirement identity also has finalized GC residue")
	}

	switch {
	case len(evidence.active) == 0 && len(evidence.controls) == 0 && len(evidence.residues) == 0:
		if len(evidence.garbage) != 0 {
			return Decision{state: StateFinalized}
		}
		return Decision{state: StateClean}
	case len(evidence.active) == 1 && len(evidence.controls) == 0 && len(evidence.residues) == 0:
		return Decision{state: StateActive}
	case len(evidence.active) == 1 && len(evidence.controls) == 1 && len(evidence.residues) == 0:
		if !active.Equal(control.record.identity) {
			return blocked("active journal and retirement control identities do not match")
		}
		if control.record.phase != PhasePrepared {
			return blocked("active journal control must remain in prepared phase")
		}
		return Decision{state: StatePrepared}
	case len(evidence.active) == 0 && len(evidence.controls) == 1:
		if len(evidence.residues) == 1 &&
			!evidence.residues[0].journalIdentity.Equal(control.record.identity) {
			return blocked("journal retirement residue does not match its control")
		}
		switch control.record.phase {
		case PhasePrepared:
			if len(evidence.residues) != 1 {
				return blocked("prepared retirement control requires its journal residue")
			}
			return cleanupDecision(StateRetained, control, true)
		case PhaseFinalizing:
			return cleanupDecision(StateFinalizing, control, len(evidence.residues) == 1)
		default:
			return blocked("journal retirement control has an unsupported phase")
		}
	default:
		return blocked("journal retirement artifacts form an unsupported state")
	}
}

func firstBlockerDetail(blockers []Blocker) string {
	if len(blockers) == 0 {
		return ""
	}
	details := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		if strings.TrimSpace(blocker.name) == "" || strings.TrimSpace(blocker.detail) == "" {
			details = append(details, "retirement inventory contains an invalid blocker")
			continue
		}
		details = append(details, blocker.detail)
	}
	slices.Sort(details)
	return details[0]
}

func matchingGC(
	active Identity,
	control Control,
	residues []Residue,
	garbage []Garbage,
) bool {
	for _, artifact := range garbage {
		switch {
		case active.valid() && artifact.name.BelongsTo(active):
			return true
		case control.record.identity.valid() && artifact.name.BelongsTo(control.record.identity):
			return true
		case len(residues) == 1:
			digest, _ := residues[0].name.Digest()
			gcDigest, _ := artifact.name.Digest()
			if digest == gcDigest {
				return true
			}
		}
	}
	return false
}

func blocked(detail string) Decision {
	return Decision{state: StateBlocked, detail: detail}
}

func cleanupDecision(state State, control Control, residuePresent bool) Decision {
	authority := CleanupAuthority{
		record:         control.record,
		residuePresent: residuePresent,
	}
	if !authority.valid() {
		return blocked("journal retirement cleanup authority is uninitialized")
	}
	plan := CleanupPlan{authority: authority}
	return Decision{state: state, cleanup: &plan}
}

func (decision Decision) valid() bool {
	switch decision.state {
	case StateClean, StateActive, StatePrepared, StateFinalized:
		return decision.cleanup == nil && decision.detail == ""
	case StateRetained, StateFinalizing:
		return decision.cleanup != nil && decision.cleanup.valid() && decision.detail == ""
	case StateBlocked:
		return decision.cleanup == nil && strings.TrimSpace(decision.detail) != ""
	default:
		return false
	}
}

// State returns the closed layout classification.
func (decision Decision) State() State {
	if !decision.valid() {
		return StateBlocked
	}
	return decision.state
}

// Blocked reports whether the inventory cannot be safely interpreted.
func (decision Decision) Blocked() bool {
	return !decision.valid() || decision.state == StateBlocked
}

// Detail returns a deterministic blocker diagnostic.
func (decision Decision) Detail() string {
	if !decision.valid() {
		return "journal retirement decision is uninitialized"
	}
	return decision.detail
}

// CleanupPlan returns cleanup-only semantic authority for retained and
// finalizing states. Physical root and entry capabilities remain external.
func (decision Decision) CleanupPlan() (CleanupPlan, bool) {
	if !decision.valid() || decision.cleanup == nil {
		return CleanupPlan{}, false
	}
	return *decision.cleanup, true
}
