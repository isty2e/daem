package retirement

import (
	"fmt"
	"strings"
)

type residueProof uint8

const (
	residueProofInvalid residueProof = iota
	residueProofPhysical
	residueProofJournal
)

// Residue is one private retired-journal observation. Physical evidence alone
// carries no cleanup authority; a prepared control additionally requires the
// residue journal to reproduce the complete recorded identity.
type Residue struct {
	name            Name
	journalIdentity Identity
	proof           residueProof
}

// ValidatePartialResidue validates only the physical residue shape. This form
// is admissible solely when a durable finalizing control already grants exact
// cleanup authority.
func ValidatePartialResidue(evidence EntryEvidence) (Residue, error) {
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
	return Residue{name: name, proof: residueProofPhysical}, nil
}

// ValidateResidue correlates one stable residue entry with identity derived
// independently from its validated journal content.
func ValidateResidue(evidence EntryEvidence, journalIdentity Identity) (Residue, error) {
	residue, err := ValidatePartialResidue(evidence)
	if err != nil {
		return Residue{}, err
	}
	if !journalIdentity.valid() {
		return Residue{}, fmt.Errorf("journal retirement residue identity is uninitialized")
	}
	if !residue.name.BelongsTo(journalIdentity) {
		return Residue{}, fmt.Errorf(
			"journal retirement residue %q does not match its loaded journal identity",
			evidence.name,
		)
	}
	residue.journalIdentity = journalIdentity
	residue.proof = residueProofJournal
	return residue, nil
}

func (residue Residue) valid() bool {
	if residue.name.kind != NameResidue || !residue.name.valid() {
		return false
	}
	switch residue.proof {
	case residueProofPhysical:
		return !residue.journalIdentity.valid()
	case residueProofJournal:
		return residue.journalIdentity.valid() &&
			residue.name.BelongsTo(residue.journalIdentity)
	default:
		return false
	}
}

func (residue Residue) independentlyMatches(identity Identity) bool {
	return residue.valid() &&
		residue.proof == residueProofJournal &&
		residue.journalIdentity.equal(identity)
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

// BlockerForName maps obsolete and malformed reserved names to fail-closed
// facts. Pre-1.0 tombstones require the daem version that wrote them.
func BlockerForName(name Name) (Blocker, bool) {
	if !name.valid() {
		return Blocker{
			name:   name.value,
			detail: "journal retirement name observation is uninitialized",
		}, true
	}
	switch name.kind {
	case nameLegacyTombstone:
		return Blocker{
			name: name.value,
			detail: fmt.Sprintf(
				"pre-1.0 journal tombstone %q uses an unsupported authority schema; use the daem version that wrote it to recover before upgrading",
				name.value,
			),
		}, true
	case nameMalformed:
		return Blocker{
			name:   name.value,
			detail: fmt.Sprintf("malformed reserved journal retirement entry %q", name.value),
		}, true
	default:
		return Blocker{}, false
	}
}
