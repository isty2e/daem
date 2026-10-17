package clipresent

import (
	"github.com/isty2e/daem/internal/desired/entity"
)

// ResourceKindName uses authoring vocabulary without implying a command route.
func ResourceKindName(kind entity.Kind) string {
	switch kind {
	case entity.KindInstructions:
		return "instruction"
	case entity.KindMCPServer:
		return "mcp-server"
	default:
		return string(kind)
	}
}

// ResourceReference leaves names literal; callers retain their absence policy.
func ResourceReference(kind entity.Kind, name string) string {
	return ResourceKindName(kind) + "/" + name
}

func authoringResourceKindName(kind AuthoringResourceKind) string {
	if kind == AuthoringResourceSkillGroup {
		return "skill-group"
	}
	return ResourceKindName(entity.Kind(kind))
}
