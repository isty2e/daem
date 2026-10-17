package authoring

import (
	"fmt"
	"os"
	"path/filepath"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/delegate"
	mcpdelegate "github.com/isty2e/daem/internal/realization/delegate/mcp"
)

// Warning retains optional resource context separately from causal text.
type Warning struct {
	Resource entity.ID
	Detail   string
}

func plainWarnings(messages []string) []Warning {
	if messages == nil {
		return nil
	}
	warnings := make([]Warning, 0, len(messages))
	for _, message := range messages {
		warnings = append(warnings, Warning{Detail: message})
	}
	return warnings
}

func skillWarnings(skill declarationcodec.Skill, manifestRoot string) []string {
	if skill.Source.Git != "" {
		return nil
	}
	localPath := localSourcePath(skill.Source, manifestRoot)
	if _, err := os.Stat(localPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return []string{fmt.Sprintf("local source %q cannot be checked now: %v; lock validates local source contents", skill.Source.Path, err)}
	}
	return []string{fmt.Sprintf("local source %q does not exist yet; lock validates local source contents", skill.Source.Path)}
}

func skillGroupWarnings(group declarationcodec.SkillGroup, manifestRoot string) []string {
	if group.Source.Git != "" {
		return nil
	}
	localPath := localSkillGroupSourcePath(group.Source, manifestRoot)
	if _, err := os.Stat(localPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return []string{fmt.Sprintf("local source root %q cannot be checked now: %v; lock validates local source contents", group.Source.Path, err)}
	}
	return []string{fmt.Sprintf("local source root %q does not exist yet; lock validates local source contents", group.Source.Path)}
}

func instructionWarnings(instruction declarationcodec.Instruction, manifestRoot string) []string {
	if instruction.Source.Git != "" || instruction.Source.S3 != "" {
		return nil
	}
	localPath := localInstructionSourcePath(instruction.Source, manifestRoot)
	info, err := os.Stat(localPath)
	if err == nil && !info.IsDir() {
		return nil
	}
	if err == nil && info.IsDir() {
		return []string{fmt.Sprintf("local instruction source %q is a directory; lock validates instruction source files", instruction.Source.Path)}
	}
	if !os.IsNotExist(err) {
		return []string{fmt.Sprintf("local instruction source %q cannot be checked now: %v; lock validates instruction source files", instruction.Source.Path, err)}
	}
	return []string{fmt.Sprintf("local instruction source %q does not exist yet; lock validates instruction source files", instruction.Source.Path)}
}

func mcpServerAuthoringWarnings(server declarationcodec.MCPServer) ([]Warning, error) {
	canonical, binding, err := canonicalMCPServerAuthoring(server)
	if err != nil {
		return nil, err
	}
	stdio, ok := binding.Transport().Stdio()
	if !ok {
		return nil, nil
	}
	plan, err := mcpdelegate.MCPStdioDelegatePlan(stdio)
	if err != nil || plan.PinPolicy() != delegate.PinFloating {
		return nil, nil
	}
	return []Warning{{Resource: canonical.ID(), Detail: floatingMCPServerDelegateWarning(plan)}}, nil
}

func floatingMCPServerDelegateWarning(plan delegate.DelegatePlan) string {
	refs := plan.PackageRefs()
	if len(refs) == 0 {
		return "uses floating delegated package identity; pin the package selector when reproducibility matters"
	}
	for _, ref := range refs {
		if ref.PinPolicy() == delegate.PinFloating {
			return fmt.Sprintf("uses floating delegated %s package %q; pin every package selector when reproducibility matters", ref.Ecosystem(), ref.Name())
		}
	}
	return "has package inputs that cannot be fully pinned from argv; use only explicit exact package selectors when reproducibility matters"
}

func localSourcePath(source declarationcodec.SkillSource, manifestRoot string) string {
	pathValue := filepath.FromSlash(source.Path)
	if filepath.IsAbs(pathValue) {
		return filepath.Clean(pathValue)
	}
	return filepath.Join(manifestRoot, pathValue)
}

func localSkillGroupSourcePath(source declarationcodec.SkillSource, manifestRoot string) string {
	pathValue := filepath.FromSlash(source.Path)
	if filepath.IsAbs(pathValue) {
		return filepath.Clean(pathValue)
	}
	return filepath.Join(manifestRoot, pathValue)
}

func localInstructionSourcePath(source declarationcodec.InstructionSource, manifestRoot string) string {
	pathValue := filepath.FromSlash(source.Path)
	if filepath.IsAbs(pathValue) {
		return filepath.Clean(pathValue)
	}
	return filepath.Join(manifestRoot, pathValue)
}
