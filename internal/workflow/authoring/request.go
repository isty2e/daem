package authoring

import (
	"fmt"
	"path"
	"strings"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
)

type AddSkillRequest struct {
	SourceArg  string
	SourcePath string
	Ref        string
	ID         string
	Name       string
	Targets    []string
	Scope      string
	Mode       string
}

type RemoveSkillRequest struct {
	ResourceKey string
	Targets     []string
	Scope       string
}

type AddSkillGroupRequest struct {
	SourceArg  string
	SourcePath string
	Ref        string
	Names      []string
	Targets    []string
	Scope      string
	Mode       string
}

type AddInstructionRequest struct {
	Name      string
	SourceArg string
	Targets   []string
	Scope     string
}

type RemoveInstructionRequest struct {
	ResourceName string
	Targets      []string
	Scope        string
}

type AddHookRequest struct {
	Name            string
	Event           string
	Command         string
	Matcher         string
	TimeoutSeconds  int
	StatusMessage   string
	Targets         []string
	Scope           string
	TargetOverrides []declarationcodec.HookTargetOverride
}

type RemoveHookRequest struct {
	ResourceName string
	Targets      []string
	Scope        string
}

type AddMCPServerRequest struct {
	Name    string
	Command string
	Args    []string
	Env     []MCPServerEnvAssignment
	Targets []string
	Scope   string
}

type RemoveMCPServerRequest struct {
	Name    string
	Targets []string
	Scope   string
}

type MCPServerEnvAssignment struct {
	Name    string
	FromEnv string
}

type AddExtensionRequest struct {
	ID      string
	Source  string
	Targets []string
	Scope   string
}

type RemoveExtensionRequest struct {
	ID      string
	Targets []string
	Scope   string
}

// CleanHookName normalizes a CLI-authored hook resource name.
func CleanHookName(value string) (string, error) {
	return cleanSafeSegment(value, "hook name")
}

// CleanInstructionName normalizes a CLI-authored instruction resource name.
func CleanInstructionName(value string) (string, error) {
	return cleanSafeSegment(value, "instruction name")
}

// CleanSkillGroupName normalizes one CLI-authored skill-group member name.
func CleanSkillGroupName(value string) (string, error) {
	return cleanSafeSegment(value, "skill-group member")
}

// CleanExtensionID normalizes a CLI-authored extension declaration ID.
func CleanExtensionID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if err := validateAuthoringStableToken(id, "extension id"); err != nil {
		return "", err
	}
	return id, nil
}

// CleanMCPServerName normalizes a CLI-authored MCP server name.
func CleanMCPServerName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if err := validateMCPStableToken(name, "mcp_server name"); err != nil {
		return "", err
	}
	return name, nil
}

func cleanSafeSegment(value string, label string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "~") ||
		strings.Contains(name, "/") || strings.Contains(name, "\\") || path.Clean(name) != name {
		return "", fmt.Errorf("%s must be a safe single path segment", label)
	}
	return name, nil
}

func cleanSkillGroupNames(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one skill-group member is required")
	}

	names := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		name, err := CleanSkillGroupName(value)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[name]; exists {
			return nil, fmt.Errorf("duplicate skill_group name %q", name)
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}
