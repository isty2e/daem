package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	adopt "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	skipMissing    = "missing"
	skipNotRegular = "not_regular_file"
)

// Candidates imports only admitted standalone MCP config projection rows.
func Candidates(target targetpkg.Target, scope targetpkg.Scope) ([]adopt.MCPServer, []adopt.Skipped, error) {
	var importConfig func(string) ([]adopt.MCPServer, []adopt.Skipped, error)
	switch {
	case target == targetpkg.TargetClaudeCode && scope == targetpkg.ScopeProject:
		importConfig = claudeProjectCandidates
	case target == targetpkg.TargetClaudeCode && scope == targetpkg.ScopeGlobal:
		importConfig = claudeGlobalCandidates
	case target == targetpkg.TargetOpenCode && scope == targetpkg.ScopeProject:
		importConfig = openCodeProjectCandidates
	case target == targetpkg.TargetOpenCode && scope == targetpkg.ScopeGlobal:
		importConfig = openCodeGlobalCandidates
	case target == targetpkg.TargetCodex && scope == targetpkg.ScopeProject:
		importConfig = codexProjectCandidates
	case target == targetpkg.TargetCodex && scope == targetpkg.ScopeGlobal:
		importConfig = codexGlobalCandidates
	case target == targetpkg.TargetAntigravityCLI && scope == targetpkg.ScopeGlobal:
		importConfig = antigravityGlobalCandidates
	default:
		return nil, []adopt.Skipped{adopt.UnsupportedSurfaceSkip(target, scope, "mcp_server")}, nil
	}
	placement, ok := aggregate.ImplementedMCPPlacement(target, scope)
	if !ok {
		return nil, nil, fmt.Errorf("MCP import route %s/%s has no canonical placement", target, scope)
	}
	livePath, err := mcpConfigPath(placement.ConfigPath(), scope)
	if err != nil {
		return nil, nil, err
	}
	return importConfig(livePath)
}

func claudeProjectCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractClaudeProjectMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetClaudeCode,
			Scope:        targetpkg.ScopeProject,
			LivePath:     livePath + "#" + mcpcodec.ClaudeProjectMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          hostEnvReferences(projection.Env),
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func claudeGlobalCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractClaudeGlobalMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetClaudeCode,
			Scope:        targetpkg.ScopeGlobal,
			LivePath:     livePath + "#" + mcpcodec.ClaudeGlobalMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          hostEnvReferences(projection.Env),
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func openCodeProjectCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractOpenCodeProjectMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetOpenCode,
			Scope:        targetpkg.ScopeProject,
			LivePath:     livePath + "#" + mcpcodec.OpenCodeProjectMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          map[string]string{},
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func openCodeGlobalCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractOpenCodeGlobalMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetOpenCode,
			Scope:        targetpkg.ScopeGlobal,
			LivePath:     livePath + "#" + mcpcodec.OpenCodeGlobalMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          openCodeEnvReferences(projection.Environment),
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func codexProjectCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractCodexProjectMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			LivePath:     livePath + "#" + mcpcodec.CodexProjectMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          map[string]string{},
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func codexGlobalCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractCodexGlobalMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeGlobal,
			LivePath:     livePath + "#" + mcpcodec.CodexGlobalMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          sameNameEnvReferences(projection.EnvVars),
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func antigravityGlobalCandidates(livePath string) ([]adopt.MCPServer, []adopt.Skipped, error) {
	content, skip, err := readConfig(livePath)
	if err != nil || skip.Reason != "" {
		return nil, skipSlice(skip), err
	}
	projections, rejections, err := mcpcodec.ExtractAntigravityGlobalMCPServerProjections(content)
	if err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetAntigravityCLI,
			Scope:        targetpkg.ScopeGlobal,
			LivePath:     livePath + "#" + mcpcodec.AntigravityGlobalMCPContentPath(projection.ServerID),
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          map[string]string{},
		})
	}
	return servers, rejectionSkips(livePath, rejections), nil
}

func mcpConfigPath(destination output.Destination, scope targetpkg.Scope) (string, error) {
	if err := destination.ValidateScope(scope); err != nil {
		return "", fmt.Errorf("validate MCP config destination %q: %w", destination, err)
	}
	if destination.RootRole() == output.RootProject {
		return filepath.FromSlash(destination.RelativePath()), nil
	}
	livePath, err := hostpath.NewResolver("").Resolve(destination)
	if err != nil {
		return "", fmt.Errorf("resolve MCP config destination %q: %w", destination, err)
	}
	return livePath, nil
}

func readConfig(livePath string) ([]byte, adopt.Skipped, error) {
	info, err := os.Stat(livePath)
	if os.IsNotExist(err) {
		return nil, adopt.Skipped{LivePath: livePath, Reason: skipMissing}, nil
	}
	if err != nil {
		return nil, adopt.Skipped{}, fmt.Errorf("read MCP config %q: %w", livePath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, adopt.Skipped{LivePath: livePath, Reason: skipNotRegular}, nil
	}
	content, err := os.ReadFile(livePath)
	if err != nil {
		return nil, adopt.Skipped{}, fmt.Errorf("read MCP config %q: %w", livePath, err)
	}
	return content, adopt.Skipped{}, nil
}

func hostEnvReferences(env map[string]string) map[string]string {
	result := make(map[string]string, len(env))
	for key, value := range env {
		result[key] = strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}")
	}
	return result
}

func openCodeEnvReferences(environment map[string]string) map[string]string {
	result := make(map[string]string, len(environment))
	for key, value := range environment {
		result[key] = strings.TrimSuffix(strings.TrimPrefix(value, "{env:"), "}")
	}
	return result
}

func sameNameEnvReferences(names []string) map[string]string {
	result := make(map[string]string, len(names))
	for _, name := range names {
		result[name] = name
	}
	return result
}

func rejectionSkips(livePath string, rejections []mcpcodec.MCPProjectionRejection) []adopt.Skipped {
	skipped := make([]adopt.Skipped, 0, len(rejections))
	for _, rejection := range rejections {
		skipped = append(skipped, adopt.Skipped{
			LivePath: livePath + "#" + rejection.ContentPath,
			Reason:   reasonString(rejection.Reason),
		})
	}
	return skipped
}

func skipReason(err error) string {
	reason, ok := mcpcodec.MCPProjectionReasonCodeOf(err)
	if !ok {
		return "unsupported_mcp_projection"
	}
	return reasonString(reason)
}

func reasonString(reason mcpcodec.MCPProjectionReasonCode) string {
	switch reason {
	case mcpcodec.MCPProjectionReasonConfigMalformed:
		return "mcp_config_malformed"
	case mcpcodec.MCPProjectionReasonUnsupportedTransport:
		return "unsupported_mcp_transport"
	case mcpcodec.MCPProjectionReasonUnsupportedManagedField:
		return "unsupported_mcp_managed_field"
	case mcpcodec.MCPProjectionReasonSecretLiteralForbidden:
		return "secret_literal_forbidden"
	case mcpcodec.MCPProjectionReasonProjectionEquivalenceUndefined:
		return "projection_equivalence_undefined"
	case mcpcodec.MCPProjectionReasonStaleAdapterContract:
		return "stale_adapter_contract"
	default:
		return "unsupported_mcp_projection"
	}
}

func skipSlice(skip adopt.Skipped) []adopt.Skipped {
	if skip.Reason == "" {
		return nil
	}
	return []adopt.Skipped{skip}
}
