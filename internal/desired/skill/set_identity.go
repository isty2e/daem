package skill

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"slices"
	"sort"
	"strings"

	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

const (
	skillSetDeclarationIdentityVersion = 2
)

// SkillSetDeclarationIdentity is opaque correlation evidence for every
// semantic fact in one normalized SkillSet declaration. It is not an EntityID.
type SkillSetDeclarationIdentity struct {
	digest [sha256.Size]byte
	valid  bool
}

// DeclarationIdentity computes the versioned canonical identity of this SkillSet.
func (set SkillSet) DeclarationIdentity() (SkillSetDeclarationIdentity, error) {
	if err := set.Validate(); err != nil {
		return SkillSetDeclarationIdentity{}, err
	}
	sourceID, err := source.SourceIDFor(set.source)
	if err != nil {
		return SkillSetDeclarationIdentity{}, fmt.Errorf("skill set declaration source: %w", err)
	}

	digest := sha256.New()
	writeSkillSetIdentityString(digest, "daem.skill-set-declaration")
	writeSkillSetIdentityUint64(digest, skillSetDeclarationIdentityVersion)
	writeSkillSetIdentityString(digest, string(sourceID))
	writeSkillSetIdentitySet(digest, selectorExpressions(set.include))
	writeSkillSetIdentitySet(digest, selectorExpressions(set.exclude))
	targets := set.targets.Values()
	targetValues := make([]string, 0, len(targets))
	for _, selected := range targets {
		targetValues = append(targetValues, string(selected))
	}
	writeSkillSetIdentitySet(digest, targetValues)
	writeSkillSetIdentityPlacements(digest, set.placements)
	writeSkillSetIdentityString(digest, string(set.scope))
	writeSkillSetIdentityString(digest, string(set.installMode))
	writeSkillSetIdentityBool(digest, set.compatRepair)
	writeSkillSetIdentityBool(digest, set.portable)

	var sum [sha256.Size]byte
	copy(sum[:], digest.Sum(nil))
	return SkillSetDeclarationIdentity{digest: sum, valid: true}, nil
}

// Validate rejects the zero declaration identity.
func (identity SkillSetDeclarationIdentity) Validate() error {
	if !identity.valid {
		return fmt.Errorf("skill set declaration identity is invalid")
	}
	return nil
}

// String returns the opaque versioned identity representation.
func (identity SkillSetDeclarationIdentity) String() string {
	if identity.Validate() != nil {
		return ""
	}
	return skillSetIdentityPrefix() + hex.EncodeToString(identity.digest[:])
}

// Equal reports whether two valid declaration identities are identical.
func (identity SkillSetDeclarationIdentity) Equal(other SkillSetDeclarationIdentity) bool {
	return identity.valid && other.valid && identity.digest == other.digest
}

// ParseSkillSetDeclarationIdentity parses the strict persisted identity form.
func ParseSkillSetDeclarationIdentity(value string) (SkillSetDeclarationIdentity, error) {
	encoded, found := strings.CutPrefix(value, skillSetIdentityPrefix())
	if !found || len(encoded) != sha256.Size*2 || strings.ToLower(encoded) != encoded {
		return SkillSetDeclarationIdentity{}, fmt.Errorf("invalid skill set declaration identity")
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil || len(decoded) != sha256.Size {
		return SkillSetDeclarationIdentity{}, fmt.Errorf("invalid skill set declaration identity")
	}

	var digest [sha256.Size]byte
	copy(digest[:], decoded)
	return SkillSetDeclarationIdentity{digest: digest, valid: true}, nil
}

func skillSetIdentityPrefix() string {
	return fmt.Sprintf("skill-set-declaration:v%d:sha256:", skillSetDeclarationIdentityVersion)
}

func selectorExpressions(selectors []Selector) []string {
	values := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		values = append(values, selector.Expression())
	}
	return values
}

func writeSkillSetIdentitySet(digest hash.Hash, values []string) {
	canonical := append([]string(nil), values...)
	sort.Strings(canonical)
	unique := canonical[:0]
	for _, value := range canonical {
		if len(unique) == 0 || unique[len(unique)-1] != value {
			unique = append(unique, value)
		}
	}
	writeSkillSetIdentityUint64(digest, uint64(len(unique)))
	for _, value := range unique {
		writeSkillSetIdentityString(digest, value)
	}
}

func writeSkillSetIdentityPlacements(
	digest hash.Hash,
	placements map[target.Target]TargetPlacement,
) {
	targets := make([]target.Target, 0, len(placements))
	for selectedTarget := range placements {
		targets = append(targets, selectedTarget)
	}
	slices.Sort(targets)
	writeSkillSetIdentityUint64(digest, uint64(len(targets)))
	for _, selectedTarget := range targets {
		writeSkillSetIdentityString(digest, string(selectedTarget))
		writeSkillSetIdentityString(digest, placements[selectedTarget].InstallTo())
	}
}

func writeSkillSetIdentityString(digest hash.Hash, value string) {
	writeSkillSetIdentityUint64(digest, uint64(len(value)))
	_, _ = digest.Write([]byte(value))
}

func writeSkillSetIdentityBool(digest hash.Hash, value bool) {
	if value {
		_, _ = digest.Write([]byte{1})
		return
	}
	_, _ = digest.Write([]byte{0})
}

func writeSkillSetIdentityUint64(digest hash.Hash, value uint64) {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	_, _ = digest.Write(encoded[:])
}
