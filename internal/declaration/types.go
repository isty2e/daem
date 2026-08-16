package declaration

import "slices"

type Kind string

const (
	KindSkill        Kind = "skill"
	KindSkillGroup   Kind = "skill_group"
	KindHook         Kind = "hook"
	KindHookAsset    Kind = "hook_asset"
	KindInstructions Kind = "instructions"
	KindMCPServer    Kind = "mcp_server"
)

type Key struct {
	Kind Kind
	Name string
}

type Targets []string

func (targets Targets) Values() []string {
	return append([]string(nil), targets...)
}

// EqualMembership reports whether two target lists contain the same values
// independent of order. Duplicate multiplicity is significant.
func (targets Targets) EqualMembership(other Targets) bool {
	if len(targets) != len(other) {
		return false
	}
	left := append([]string(nil), targets...)
	right := append([]string(nil), other...)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

// Intersects reports whether two authored target sets share a value.
func (targets Targets) Intersects(other Targets) bool {
	for _, left := range targets {
		if slices.Contains(other, left) {
			return true
		}
	}
	return false
}
