package diagnose

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/findings"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/profile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

type doctorTargetSpec struct {
	ConfigRoot  string
	ConfigFiles []doctorConfigFile
}

type doctorSkillRootSpec struct {
	Scope targetpkg.Scope
	Role  string
	Index int
	Root  string
}

func resourceKindSelected(kinds map[entity.Kind]struct{}, kind entity.Kind) bool {
	_, ok := kinds[kind]
	return ok
}

func targetChecks(
	ctx context.Context,
	homeDirectory string,
	manifestRoot string,
	projectPlacementAllowed bool,
	target targetpkg.Target,
	resourceKinds map[entity.Kind]struct{},
) []findings.Check {
	checks := targetCapabilityChecks(target, resourceKinds)
	spec := targetSpecFor(homeDirectory, target)
	if spec.ConfigRoot == "" && resourceKindSelected(resourceKinds, entity.KindHook) {
		checks = append(checks, warnCheck(fmt.Sprintf("target=%s", target), "target diagnostics are not implemented"))
	}
	if spec.ConfigRoot != "" && resourceKindSelected(resourceKinds, entity.KindHook) {
		checks = append(checks, directoryCheck(fmt.Sprintf("target=%s config_dir", target), spec.ConfigRoot))
		for _, configFile := range spec.ConfigFiles {
			checks = append(checks, configFileCheck(ctx, fmt.Sprintf("target=%s config_file", target), configFile))
		}
	}
	if resourceKindSelected(resourceKinds, entity.KindSkill) {
		checks = append(checks, skillRootChecks(homeDirectory, manifestRoot, projectPlacementAllowed, target)...)
	}

	return checks
}

func independentTargetChecks(
	ctx context.Context,
	homeDirectory string,
	manifestRoot string,
	projectPlacementAllowed bool,
	target targetpkg.Target,
	resourceKinds map[entity.Kind]struct{},
) []findings.Check {
	checks := targetCapabilityChecks(target, resourceKinds)
	spec := targetSpecFor(homeDirectory, target)
	if spec.ConfigRoot == "" && resourceKindSelected(resourceKinds, entity.KindHook) {
		checks = append(checks, warnCheck(fmt.Sprintf("target=%s", target), "target diagnostics are not implemented"))
	}
	if spec.ConfigRoot != "" && resourceKindSelected(resourceKinds, entity.KindHook) {
		checks = append(checks, unsupportedCheck(
			fmt.Sprintf("target=%s config_dir", target),
			"host config-directory readiness cannot be honored on this platform",
		))
		for _, configFile := range spec.ConfigFiles {
			checks = append(checks, configFileCheck(ctx, fmt.Sprintf("target=%s config_file", target), configFile))
		}
	}
	if resourceKindSelected(resourceKinds, entity.KindSkill) {
		checks = append(checks, independentSkillRootChecks(projectPlacementAllowed, target)...)
	}

	return checks
}

func independentSkillRootChecks(projectPlacementAllowed bool, target targetpkg.Target) []findings.Check {
	if !hostsurfacecatalog.Product().HasManagedPathTarget(target, entity.KindSkill) {
		return nil
	}

	rootSpecs := skillRootSpecs(target, targetpkg.ScopeGlobal)
	if projectPlacementAllowed {
		rootSpecs = append(rootSpecs, skillRootSpecs(target, targetpkg.ScopeProject)...)
	}

	checks := make([]findings.Check, 0, len(rootSpecs))
	for _, rootSpec := range rootSpecs {
		if strings.TrimSpace(rootSpec.Root) == "" {
			continue
		}
		checks = append(checks, unsupportedCheck(
			skillRootCheckName(target, rootSpec),
			"skill-root readiness cannot be honored on this platform",
		))
	}

	return checks
}

func targetCapabilityChecks(target targetpkg.Target, resourceKinds map[entity.Kind]struct{}) []findings.Check {
	supports := hostsurfacecatalog.Product().ResourceSupportsForTarget(target)
	checks := make([]findings.Check, 0, len(supports))
	for _, support := range supports {
		if !resourceKindSelected(resourceKinds, support.ResourceKind()) {
			continue
		}
		name := fmt.Sprintf("target=%s capability=%s", support.Target(), support.ResourceKind())
		detail := targetSupportDetail(support)
		if support.Supported() {
			checks = append(checks, okCheck(name, detail))
			continue
		}
		checks = append(checks, warnCheck(name, detail))
	}

	return checks
}

func targetSpecFor(homeDirectory string, target targetpkg.Target) doctorTargetSpec {
	switch target {
	case targetpkg.TargetCodex:
		configRoot := filepath.Join(homeDirectory, ".codex")
		return doctorTargetSpec{
			ConfigRoot: configRoot,
			ConfigFiles: []doctorConfigFile{
				{
					Path:                filepath.Join(configRoot, "config.toml"),
					Format:              ConfigFormatTOML,
					SyntaxErrorSeverity: findings.SeverityError,
				},
			},
		}
	case targetpkg.TargetClaudeCode:
		configRoot := filepath.Join(homeDirectory, ".claude")
		return doctorTargetSpec{
			ConfigRoot: configRoot,
			ConfigFiles: []doctorConfigFile{
				{
					Path:                filepath.Join(configRoot, "settings.json"),
					Format:              ConfigFormatJSON,
					SyntaxErrorSeverity: findings.SeverityError,
				},
			},
		}
	case targetpkg.TargetOpenCode:
		configRoot := filepath.Join(homeDirectory, ".config", "opencode")
		return doctorTargetSpec{
			ConfigRoot: configRoot,
			ConfigFiles: []doctorConfigFile{
				{
					Path:                filepath.Join(configRoot, "opencode.json"),
					Format:              ConfigFormatJSON,
					SyntaxErrorSeverity: findings.SeverityWarn,
				},
			},
		}
	case targetpkg.TargetPi:
		return doctorTargetSpec{
			ConfigRoot: filepath.Join(homeDirectory, ".pi", "agent"),
		}
	case targetpkg.TargetAntigravityCLI:
		configRoot := filepath.Join(homeDirectory, ".gemini", "antigravity-cli")
		return doctorTargetSpec{
			ConfigRoot: configRoot,
			ConfigFiles: []doctorConfigFile{
				{
					Path:                filepath.Join(configRoot, "settings.json"),
					Format:              ConfigFormatJSON,
					SyntaxErrorSeverity: findings.SeverityWarn,
				},
			},
		}
	default:
		return doctorTargetSpec{}
	}
}

func skillRootChecks(homeDirectory string, manifestRoot string, projectPlacementAllowed bool, target targetpkg.Target) []findings.Check {
	if !hostsurfacecatalog.Product().HasManagedPathTarget(target, entity.KindSkill) {
		return nil
	}

	rootSpecs := skillRootSpecs(target, targetpkg.ScopeGlobal)
	if projectPlacementAllowed {
		rootSpecs = append(rootSpecs, skillRootSpecs(target, targetpkg.ScopeProject)...)
	}

	checks := make([]findings.Check, 0, len(rootSpecs))
	for _, rootSpec := range rootSpecs {
		if strings.TrimSpace(rootSpec.Root) == "" {
			continue
		}
		checks = append(checks, directoryCheck(
			skillRootCheckName(target, rootSpec),
			skillRootPath(homeDirectory, manifestRoot, rootSpec),
		))
	}

	return checks
}

func skillRootSpecs(selectedTarget targetpkg.Target, scope targetpkg.Scope) []doctorSkillRootSpec {
	compiled := hostsurfacecatalog.Product()
	result := make([]doctorSkillRootSpec, 0)
	if defaultView, ok := compiled.ManagedPathDefault(selectedTarget, scope, entity.KindSkill); ok {
		result = append(result, doctorSkillRootSpec{
			Scope: scope, Role: "preferred", Index: -1, Root: defaultView.Placement().Root().String(),
		})
	}
	compatibleIndex := 0
	for _, location := range compiled.ManagedPathDiscoveryLocations(selectedTarget, scope, entity.KindSkill) {
		if view, admitted := compiled.ManagedPathAt(
			selectedTarget,
			scope,
			entity.KindSkill,
			location.Path(),
		); admitted && view.IsDefaultPlacement() {
			continue
		}
		result = append(result, doctorSkillRootSpec{
			Scope: scope, Role: "compatible", Index: compatibleIndex, Root: location.Path(),
		})
		compatibleIndex++
	}
	return result
}

func targetSupportDetail(support profile.Support) string {
	action := resourceAction(support.ResourceKind())
	if support.Supported() {
		if support.ResourceKind() == entity.KindInstructions {
			scopes := defaultPlacementScopes(support.Target(), entity.KindInstructions)
			switch len(scopes) {
			case 0:
				return fmt.Sprintf("%s has no default placement scope", action)
			case 1:
				return fmt.Sprintf("%s is supported for %s scope", action, scopes[0])
			}
		}
		return fmt.Sprintf("%s is supported", action)
	}
	switch support.Reason() {
	case profile.UnsupportedReasonBridgeRequired:
		return fmt.Sprintf("%s requires an extension bridge surface", action)
	default:
		return fmt.Sprintf("%s is not implemented", action)
	}
}

func defaultPlacementScopes(selectedTarget targetpkg.Target, resourceKind entity.Kind) []targetpkg.Scope {
	compiled := hostsurfacecatalog.Product()
	scopes := make([]targetpkg.Scope, 0, 2)
	for _, scope := range []targetpkg.Scope{targetpkg.ScopeProject, targetpkg.ScopeGlobal} {
		if _, ok := compiled.ManagedPathDefault(selectedTarget, scope, resourceKind); ok {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func resourceAction(resourceKind entity.Kind) string {
	switch resourceKind {
	case "":
		return "resource reconciliation"
	case entity.KindInstructions:
		return "instruction rendering"
	case entity.KindSkill:
		return "skill reconciliation"
	case entity.KindHook:
		return "command hook reconciliation"
	default:
		return fmt.Sprintf("%s reconciliation", resourceKind)
	}
}

func skillRootCheckName(target targetpkg.Target, rootSpec doctorSkillRootSpec) string {
	role := rootSpec.Role
	if rootSpec.Index >= 0 {
		role = fmt.Sprintf("%s[%d]", rootSpec.Role, rootSpec.Index)
	}

	return fmt.Sprintf("target=%s skill_root=%s %s", target, rootSpec.Scope, role)
}

func skillRootPath(homeDirectory string, manifestRoot string, rootSpec doctorSkillRootSpec) string {
	if after, ok := strings.CutPrefix(rootSpec.Root, "~/"); ok {
		return filepath.Join(homeDirectory, after)
	}
	if filepath.IsAbs(rootSpec.Root) {
		return filepath.Clean(rootSpec.Root)
	}
	if rootSpec.Scope == targetpkg.ScopeProject {
		return filepath.Join(manifestRoot, filepath.FromSlash(rootSpec.Root))
	}

	return filepath.Clean(rootSpec.Root)
}
