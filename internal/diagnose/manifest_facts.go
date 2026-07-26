package diagnose

import (
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	hookresource "github.com/isty2e/daem/internal/desired/hook"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	skillresource "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

// ManifestFacts is the immutable doctor projection of one normalized manifest.
type ManifestFacts struct {
	contextSelection targetselection.Selection
	resourceKinds    map[entity.Kind]struct{}
	skills           []skillresource.Skill
	hooks            []hookresource.Hook
	skillSets        []skillresource.SkillSet
	mcpServers       []desiredmcp.Server
}

// NewManifestFacts derives doctor facts from one normalized manifest.
func NewManifestFacts(environment desired.Environment) (ManifestFacts, error) {
	if err := environment.Validate(); err != nil {
		return ManifestFacts{}, fmt.Errorf("manifest: %w", err)
	}
	selection, err := contextSelection(environment)
	if err != nil {
		return ManifestFacts{}, err
	}

	return ManifestFacts{
		contextSelection: selection,
		resourceKinds:    classifyResourceKinds(environment),
		skills:           environment.Skills(),
		hooks:            environment.Hooks(),
		skillSets:        environment.SkillSets(),
		mcpServers:       environment.MCPServers(),
	}, nil
}

// ContextSelection returns the targets implied by the manifest for doctor checks.
func (facts ManifestFacts) ContextSelection() targetselection.Selection {
	return facts.contextSelection
}

// ResourceKinds returns the manifest resource families inspected by doctor.
func (facts ManifestFacts) ResourceKinds() map[entity.Kind]struct{} {
	cloned := make(map[entity.Kind]struct{}, len(facts.resourceKinds))
	for kind := range facts.resourceKinds {
		cloned[kind] = struct{}{}
	}
	return cloned
}

// Skills returns direct canonical skill resources.
func (facts ManifestFacts) Skills() []skillresource.Skill {
	return cloneSkills(facts.skills)
}

// Hooks returns direct canonical hook resources.
func (facts ManifestFacts) Hooks() []hookresource.Hook {
	return cloneHooks(facts.hooks)
}

// SkillSets returns canonical selector-backed skill generators.
func (facts ManifestFacts) SkillSets() []skillresource.SkillSet {
	return slices.Clone(facts.skillSets)
}

// MCPServers returns canonical MCP servers used by executable diagnostics.
func (facts ManifestFacts) MCPServers() []desiredmcp.Server {
	return slices.Clone(facts.mcpServers)
}

func contextSelection(environment desired.Environment) (targetselection.Selection, error) {
	selected := make(map[target.Target]struct{})
	if err := addContextTargets(selected, environment.Targets(), "manifest targets"); err != nil {
		return targetselection.Selection{}, err
	}
	for _, skill := range environment.Skills() {
		if err := addContextTargets(selected, skill.Targets(), fmt.Sprintf("skill %q targets", skill.ID().Name())); err != nil {
			return targetselection.Selection{}, err
		}
	}
	for _, hook := range environment.Hooks() {
		if err := addContextTargets(selected, hook.Targets(), fmt.Sprintf("hook %q targets", hook.ID().Name())); err != nil {
			return targetselection.Selection{}, err
		}
	}
	for _, instructions := range environment.Instructions() {
		if err := addContextTargets(selected, instructions.Targets(), fmt.Sprintf("instructions %q targets", instructions.ID().Name())); err != nil {
			return targetselection.Selection{}, err
		}
	}
	for _, server := range environment.MCPServers() {
		for _, binding := range server.Bindings() {
			if err := addContextTargets(selected, []target.Target{binding.Target()}, fmt.Sprintf("mcp_server %q target", server.ID().Name())); err != nil {
				return targetselection.Selection{}, err
			}
		}
	}

	values := make([]string, 0, len(selected))
	for _, supported := range target.SupportedTargets() {
		if _, ok := selected[supported]; ok {
			values = append(values, string(supported))
		}
	}
	return targetselection.ForDiagnostics(values)
}

func addContextTargets(selected map[target.Target]struct{}, values []target.Target, context string) error {
	for _, value := range values {
		parsed, err := target.ParseTarget(string(value))
		if err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
		selected[parsed] = struct{}{}
	}
	return nil
}

func classifyResourceKinds(environment desired.Environment) map[entity.Kind]struct{} {
	kinds := make(map[entity.Kind]struct{}, 3)
	if len(environment.Instructions()) > 0 {
		kinds[entity.KindInstructions] = struct{}{}
	}
	if len(environment.Skills()) > 0 {
		kinds[entity.KindSkill] = struct{}{}
	}
	if len(environment.Hooks()) > 0 {
		kinds[entity.KindHook] = struct{}{}
	}
	return kinds
}

func cloneSkills(values []skillresource.Skill) []skillresource.Skill {
	return slices.Clone(values)
}

func cloneHooks(values []hookresource.Hook) []hookresource.Hook {
	return slices.Clone(values)
}
