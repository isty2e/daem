package skillcompat

import "github.com/isty2e/daem/internal/target"

// profile is the canonical compatibility contract for one agent skill target.
type profile struct {
	Target        target.Target
	Artifact      artifactRules
	Discovery     discoveryRules
	Frontmatter   frontmatterRules
	Identity      identityRules
	Selection     selectionRules
	ControlFields controlFieldRules
	Collision     collisionRules
}

type artifactRules struct {
	RequiresDirectory          bool
	RequiresUppercaseSkillFile bool
}

type discoveryRules struct {
	ProjectRoots []string
	GlobalRoots  []string
	Recursive    bool
	RootMarkdown bool
}

type frontmatterRules struct {
	RequireFrontmatter       bool
	RequireName              bool
	RequireDescription       bool
	RecommendDescription     bool
	StrictName               bool
	WarnInvalidName          bool
	WarnNameLength           bool
	WarnDescriptionLength    bool
	MaxNameLength            int
	MaxDescriptionLength     int
	RequireNameMatchesFolder bool
}

type identityRules struct {
	AddressedBy string
}

type selectionRules struct {
	DescriptionAffectsSelection bool
}

type controlFieldRules struct {
	RecognizedFrontmatterFields []string
	SidecarFiles                []string
}

type collisionRules struct {
	Behavior string
}

var profileRegistry = map[target.Target]profile{
	target.TargetCodex: {
		Target: target.TargetCodex,
		Artifact: artifactRules{
			RequiresDirectory:          true,
			RequiresUppercaseSkillFile: true,
		},
		Discovery: discoveryRules{
			ProjectRoots: []string{".agents/skills"},
			GlobalRoots:  []string{"~/.agents/skills", "~/.codex/skills", "/etc/codex/skills"},
		},
		Frontmatter: frontmatterRules{
			RequireFrontmatter: true,
			RequireName:        true,
			RequireDescription: true,
		},
		Identity: identityRules{
			AddressedBy: "frontmatter name and discovered file path",
		},
		Selection: selectionRules{
			DescriptionAffectsSelection: true,
		},
		ControlFields: controlFieldRules{
			RecognizedFrontmatterFields: standardAgentSkillFields(),
			SidecarFiles:                []string{"agents/openai.yaml"},
		},
		Collision: collisionRules{
			Behavior: "target-defined first match",
		},
	},
	target.TargetClaudeCode: {
		Target: target.TargetClaudeCode,
		Artifact: artifactRules{
			RequiresDirectory:          true,
			RequiresUppercaseSkillFile: true,
		},
		Discovery: discoveryRules{
			ProjectRoots: []string{".claude/skills"},
			GlobalRoots:  []string{"~/.claude/skills"},
		},
		Frontmatter: frontmatterRules{
			RequireFrontmatter:   true,
			RecommendDescription: true,
		},
		Identity: identityRules{
			AddressedBy: "directory name, with optional frontmatter display name",
		},
		Selection: selectionRules{
			DescriptionAffectsSelection: true,
		},
		ControlFields: controlFieldRules{
			RecognizedFrontmatterFields: []string{
				"name",
				"description",
				"license",
				"compatibility",
				"metadata",
				"when_to_use",
				"argument-hint",
				"arguments",
				"disable-model-invocation",
				"user-invocable",
				"allowed-tools",
				"model",
				"effort",
				"context",
				"agent",
				"hooks",
				"paths",
				"shell",
			},
		},
		Collision: collisionRules{
			Behavior: "scope precedence and command name resolution",
		},
	},
	target.TargetOpenCode: {
		Target: target.TargetOpenCode,
		Artifact: artifactRules{
			RequiresDirectory:          true,
			RequiresUppercaseSkillFile: true,
		},
		Discovery: discoveryRules{
			ProjectRoots: []string{".opencode/skills", ".claude/skills", ".agents/skills"},
			GlobalRoots:  []string{"~/.config/opencode/skills", "~/.claude/skills", "~/.agents/skills"},
		},
		Frontmatter: frontmatterRules{
			RequireFrontmatter:       true,
			RequireName:              true,
			RequireDescription:       true,
			StrictName:               true,
			MaxNameLength:            64,
			MaxDescriptionLength:     1024,
			RequireNameMatchesFolder: true,
		},
		Identity: identityRules{
			AddressedBy: "frontmatter name matching parent directory",
		},
		Selection: selectionRules{
			DescriptionAffectsSelection: true,
		},
		ControlFields: controlFieldRules{
			RecognizedFrontmatterFields: []string{"name", "description", "license", "compatibility", "metadata"},
		},
		Collision: collisionRules{
			Behavior: "unique skill names required",
		},
	},
	target.TargetPi: {
		Target: target.TargetPi,
		Artifact: artifactRules{
			RequiresDirectory:          true,
			RequiresUppercaseSkillFile: true,
		},
		Discovery: discoveryRules{
			ProjectRoots: []string{".pi/skills", ".agents/skills"},
			GlobalRoots:  []string{"~/.pi/agent/skills", "~/.agents/skills"},
			Recursive:    true,
			RootMarkdown: true,
		},
		Frontmatter: frontmatterRules{
			RequireFrontmatter:    true,
			RequireName:           true,
			RequireDescription:    true,
			WarnInvalidName:       true,
			WarnNameLength:        true,
			WarnDescriptionLength: true,
			MaxNameLength:         64,
			MaxDescriptionLength:  1024,
		},
		Identity: identityRules{
			AddressedBy: "frontmatter name; parent directory may differ",
		},
		Selection: selectionRules{
			DescriptionAffectsSelection: true,
		},
		ControlFields: controlFieldRules{
			RecognizedFrontmatterFields: []string{
				"name",
				"description",
				"license",
				"compatibility",
				"metadata",
				"allowed-tools",
				"disable-model-invocation",
			},
		},
		Collision: collisionRules{
			Behavior: "warn and keep first discovered skill",
		},
	},
	target.TargetAntigravityCLI: {
		Target: target.TargetAntigravityCLI,
		Artifact: artifactRules{
			RequiresDirectory:          true,
			RequiresUppercaseSkillFile: true,
		},
		Discovery: discoveryRules{
			ProjectRoots: []string{".agents/skills"},
			GlobalRoots:  []string{"~/.gemini/config/skills"},
		},
		Frontmatter: frontmatterRules{
			RequireFrontmatter: true,
			RequireName:        true,
			RequireDescription: true,
		},
		Identity: identityRules{
			AddressedBy: "frontmatter name; parent directory may differ",
		},
		Selection: selectionRules{
			DescriptionAffectsSelection: true,
		},
		ControlFields: controlFieldRules{
			RecognizedFrontmatterFields: standardAgentSkillFields(),
		},
		Collision: collisionRules{
			Behavior: "target-defined registered skill catalog",
		},
	},
}

func profileForTarget(target target.Target) (profile, bool) {
	selected, ok := profileRegistry[target]
	if !ok {
		return profile{}, false
	}

	return copyProfile(selected), true
}

func copyProfile(profile profile) profile {
	profile.Discovery.ProjectRoots = append([]string(nil), profile.Discovery.ProjectRoots...)
	profile.Discovery.GlobalRoots = append([]string(nil), profile.Discovery.GlobalRoots...)
	profile.ControlFields.RecognizedFrontmatterFields = append(
		[]string(nil),
		profile.ControlFields.RecognizedFrontmatterFields...,
	)
	profile.ControlFields.SidecarFiles = append([]string(nil), profile.ControlFields.SidecarFiles...)
	return profile
}

func standardAgentSkillFields() []string {
	return []string{"name", "description", "license", "compatibility", "metadata", "allowed-tools"}
}
