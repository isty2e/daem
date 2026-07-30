package retirement

import (
	"fmt"
	"strings"
)

// Residue is one private retired journal whose loaded journal content
// independently reproduces the identity encoded by its name.
type Residue struct {
	name            Name
	journalIdentity Identity
}

// ValidateResidue correlates one stable residue entry with identity derived
// independently from its validated journal content.
func ValidateResidue(evidence EntryEvidence, journalIdentity Identity) (Residue, error) {
	name := InspectName(evidence.name)
	if name.kind != NameResidue {
		return Residue{}, fmt.Errorf(
			"entry %q is not a valid journal retirement residue name",
			evidence.name,
		)
	}
	if err := validatePrivateDirectory(evidence, "retirement artifact"); err != nil {
		return Residue{}, err
	}
	if !journalIdentity.valid() {
		return Residue{}, fmt.Errorf("journal retirement residue identity is uninitialized")
	}
	if !name.BelongsTo(journalIdentity) {
		return Residue{}, fmt.Errorf(
			"journal retirement residue %q does not match its loaded journal identity",
			evidence.name,
		)
	}
	return Residue{name: name, journalIdentity: journalIdentity}, nil
}

// Garbage is one validated, inert post-finalization GC directory.
type Garbage struct {
	name Name
}

// ValidateGarbage converts stable filesystem evidence into inert GC residue.
func ValidateGarbage(evidence EntryEvidence) (Garbage, error) {
	name := InspectName(evidence.name)
	if name.kind != NameGC {
		return Garbage{}, fmt.Errorf(
			"entry %q is not a valid journal retirement GC name",
			evidence.name,
		)
	}
	if err := validatePrivateDirectory(evidence, "retirement GC artifact"); err != nil {
		return Garbage{}, err
	}
	return Garbage{name: name}, nil
}

// Blocker is a recovery-root fact that must fail closed.
type Blocker struct {
	name   string
	detail string
}

// NewBlocker constructs one deterministic blocked inventory fact.
func NewBlocker(name string, detail string) (Blocker, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(detail) == "" {
		return Blocker{}, fmt.Errorf("retirement blocker requires name and detail")
	}
	return Blocker{name: name, detail: detail}, nil
}

// BlockerForName maps legacy and malformed reserved names to fail-closed
// facts. Unrelated hidden names remain unrelated.
func BlockerForName(name Name) (Blocker, bool) {
	if !name.valid() {
		return Blocker{
			name:   name.value,
			detail: "journal retirement name observation is uninitialized",
		}, true
	}
	switch name.kind {
	case NameLegacyTombstone:
		return Blocker{
			name: name.value,
			detail: fmt.Sprintf(
				"legacy journal tombstone %q requires manual remediation",
				name.value,
			),
		}, true
	case NameMalformed:
		return Blocker{
			name:   name.value,
			detail: fmt.Sprintf("malformed reserved journal retirement entry %q", name.value),
		}, true
	default:
		return Blocker{}, false
	}
}
