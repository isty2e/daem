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

// Intersects reports whether two authored target sets share a value.
func (targets Targets) Intersects(other Targets) bool {
	for _, left := range targets {
		if slices.Contains(other, left) {
			return true
		}
	}
	return false
}
