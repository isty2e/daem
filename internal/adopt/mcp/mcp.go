package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	adopt "github.com/isty2e/daem/internal/adopt"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	skipMissing           = "missing"
	skipNotRegular        = "not_regular_file"
	skipFinalSymlink      = "mcp_config_final_symlink"
	skipTooLarge          = "mcp_config_too_large"
	skipChangedDuringRead = "mcp_config_changed_during_read"
	skipAlternateConfig   = "unsupported_mcp_alternate_config"
	skipInvalidArgument   = "invalid_mcp_argument"
)

type importSource struct {
	primaryPath         string
	requiredAbsentPaths []string
}

type importDocument struct {
	content  []byte
	revision string
}

func newImportSource(primaryPath string, requiredAbsentPaths ...string) importSource {
	canonicalAbsentPaths := append([]string(nil), requiredAbsentPaths...)
	slices.Sort(canonicalAbsentPaths)
	canonicalAbsentPaths = slices.Compact(canonicalAbsentPaths)
	return importSource{
		primaryPath:         primaryPath,
		requiredAbsentPaths: canonicalAbsentPaths,
	}
}

func (source importSource) route(
	contentPath string,
	document importDocument,
	maximumBytes int64,
) (adopt.MCPSourceRoute, error) {
	return adopt.NewMCPSourceRoute(adopt.MCPSourceRouteInput{
		PrimaryPath:         source.primaryPath,
		PrimaryRevision:     document.revision,
		MaximumBytes:        maximumBytes,
		ContentPath:         contentPath,
		RequiredAbsentPaths: source.requiredAbsentPaths,
	})
}

func (source importSource) authority(
	target targetpkg.Target,
	scope targetpkg.Scope,
	document importDocument,
	maximumBytes int64,
) adopt.MCPSourceAuthority {
	return adopt.MCPSourceAuthority{
		Target:              target,
		Scope:               scope,
		PrimaryPath:         source.primaryPath,
		PrimaryRevision:     document.revision,
		MaximumBytes:        maximumBytes,
		RequiredAbsentPaths: append([]string(nil), source.requiredAbsentPaths...),
	}
}

type candidateHooks struct {
	afterSnapshot           func()
	beforeArgumentAdmission func(int)
	beforeSuccess           func()
}

// Candidates observes one MCP document and returns admitted standalone
// projections, exact source authority, and classified skips.
func Candidates(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, []adopt.Skipped, error) {
	return candidatesWithHooks(ctx, target, scope, candidateHooks{})
}

func candidatesWithHooks(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	hooks candidateHooks,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, []adopt.Skipped, error) {
	if ctx == nil {
		return nil, nil, nil, fmt.Errorf("MCP import context is required")
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, nil, err
	}
	var importConfig func(context.Context, importSource, importDocument, int64) ([]adopt.MCPServer, []adopt.Skipped, error)
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
		return finishCandidates(
			ctx,
			hooks,
			nil,
			nil,
			[]adopt.Skipped{adopt.UnsupportedSurfaceSkip(target, scope, "mcp_server")},
		)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(target, scope)
	if !ok {
		return nil, nil, nil, fmt.Errorf("MCP import route %s/%s has no canonical placement", target, scope)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		return nil, nil, nil, fmt.Errorf("MCP import route %s/%s has no aggregate codec", target, scope)
	}
	primaryPath, err := mcpConfigPath(placement.ConfigPath(), scope)
	if err != nil {
		return nil, nil, nil, err
	}
	requiredAbsentPaths := make([]string, 0, 1)
	if conflictingConfig, hasConflict := placement.ConflictingConfigPath(); hasConflict {
		conflictingPath, err := mcpConfigPath(conflictingConfig, scope)
		if err != nil {
			return nil, nil, nil, err
		}
		requiredAbsentPaths = append(requiredAbsentPaths, conflictingPath)
	}
	source := newImportSource(primaryPath, requiredAbsentPaths...)
	maximumBytes := codec.MaximumDocumentBytes()
	document, skip, err := readImportSource(ctx, source, maximumBytes)
	if err != nil || skip.Reason != "" {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, nil, contextErr
		}
		return nil, nil, skipSlice(skip), err
	}
	if hooks.afterSnapshot != nil {
		hooks.afterSnapshot()
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, nil, err
	}
	authorities := []adopt.MCPSourceAuthority{
		source.authority(target, scope, document, maximumBytes),
	}
	servers, skipped, err := importConfig(
		ctx,
		source,
		document,
		maximumBytes,
	)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, nil, contextErr
		}
		return nil, nil, nil, err
	}
	servers, skipped, err = admitMCPArgumentCandidates(ctx, servers, skipped, hooks.beforeArgumentAdmission)
	if err != nil {
		return nil, nil, nil, err
	}
	return finishCandidates(ctx, hooks, servers, authorities, skipped)
}

func admitMCPArgumentCandidates(
	ctx context.Context,
	servers []adopt.MCPServer,
	skipped []adopt.Skipped,
	before func(int),
) ([]adopt.MCPServer, []adopt.Skipped, error) {
	admitted := make([]adopt.MCPServer, 0, len(servers))
	classified := append([]adopt.Skipped(nil), skipped...)
	for index, server := range servers {
		if before != nil {
			before(index)
		}
		if err := mcpImportContextError(ctx); err != nil {
			return nil, nil, err
		}
		validationErr := desiredmcp.ValidateStdioArguments(server.Args)
		if err := mcpImportContextError(ctx); err != nil {
			return nil, nil, err
		}
		if validationErr != nil {
			classified = append(classified, adopt.Skipped{
				LivePath: server.LivePath(),
				Reason:   skipInvalidArgument,
			})
			continue
		}
		admitted = append(admitted, server)
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, err
	}
	return admitted, classified, nil
}

func finishCandidates(
	ctx context.Context,
	hooks candidateHooks,
	servers []adopt.MCPServer,
	authorities []adopt.MCPSourceAuthority,
	skipped []adopt.Skipped,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, []adopt.Skipped, error) {
	if hooks.beforeSuccess != nil {
		hooks.beforeSuccess()
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, nil, err
	}
	return servers, authorities, skipped, nil
}

func mcpImportContextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		if cause := context.Cause(ctx); cause != nil {
			return cause
		}
		return err
	}
	return nil
}

func claudeProjectCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractClaudeProjectMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.ClaudeProjectMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetClaudeCode,
			Scope:        targetpkg.ScopeProject,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          hostEnvReferences(projection.Env),
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
}

func claudeGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractClaudeGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.ClaudeGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetClaudeCode,
			Scope:        targetpkg.ScopeGlobal,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          hostEnvReferences(projection.Env),
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
}

func openCodeProjectCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractOpenCodeProjectMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.OpenCodeProjectMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetOpenCode,
			Scope:        targetpkg.ScopeProject,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          map[string]string{},
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
}

func openCodeGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractOpenCodeGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.OpenCodeGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetOpenCode,
			Scope:        targetpkg.ScopeGlobal,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          openCodeEnvReferences(projection.Environment),
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
}

func codexProjectCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractCodexProjectMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.CodexProjectMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeProject,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          map[string]string{},
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
}

func codexGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractCodexGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.CodexGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetCodex,
			Scope:        targetpkg.ScopeGlobal,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          sameNameEnvReferences(projection.EnvVars),
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
}

func antigravityGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64) ([]adopt.MCPServer, []adopt.Skipped, error) {
	projections, rejections, err := mcpcodec.ExtractAntigravityGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, []adopt.Skipped{{LivePath: source.primaryPath, Reason: skipReason(err)}}, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		route, err := source.route(mcpcodec.AntigravityGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, nil, err
		}
		servers = append(servers, adopt.MCPServer{
			ResourceName: projection.ServerID,
			Target:       targetpkg.TargetAntigravityCLI,
			Scope:        targetpkg.ScopeGlobal,
			SourceRoute:  route,
			Command:      projection.Command,
			Args:         append([]string(nil), projection.Args...),
			Env:          map[string]string{},
		})
	}
	return servers, rejectionSkips(source.primaryPath, rejections), mcpImportContextError(ctx)
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

func readImportSource(
	ctx context.Context,
	source importSource,
	maximumBytes int64,
) (importDocument, adopt.Skipped, error) {
	for _, requiredAbsentPath := range source.requiredAbsentPaths {
		if err := ctx.Err(); err != nil {
			return importDocument{}, adopt.Skipped{}, err
		}
		_, statErr := os.Lstat(requiredAbsentPath)
		if err := ctx.Err(); err != nil {
			return importDocument{}, adopt.Skipped{}, err
		}
		switch {
		case statErr == nil:
			return importDocument{}, adopt.Skipped{
				LivePath: requiredAbsentPath,
				Reason:   skipAlternateConfig,
			}, nil
		case errors.Is(statErr, os.ErrNotExist):
			continue
		default:
			return importDocument{}, adopt.Skipped{}, fmt.Errorf(
				"inspect required-absent MCP config %q: %w",
				requiredAbsentPath,
				statErr,
			)
		}
	}
	return readConfig(ctx, source.primaryPath, maximumBytes)
}

func readConfig(ctx context.Context, livePath string, maximumBytes int64) (importDocument, adopt.Skipped, error) {
	snapshot, exists, err := filesnapshot.ReadRegularFileSnapshotContext(ctx, livePath, maximumBytes)
	if err == nil && !exists {
		return importDocument{}, adopt.Skipped{LivePath: livePath, Reason: skipMissing}, nil
	}
	if err != nil {
		if skip, ok := mcpSnapshotSkip(livePath, err); ok {
			return importDocument{}, skip, nil
		}
		return importDocument{}, adopt.Skipped{}, fmt.Errorf("read MCP config %q: %w", livePath, err)
	}
	return importDocument{
		content:  snapshot.Content(),
		revision: snapshot.Revision(),
	}, adopt.Skipped{}, nil
}

func mcpSnapshotSkip(livePath string, err error) (adopt.Skipped, bool) {
	for _, candidate := range []struct {
		match  error
		reason string
	}{
		{match: filesnapshot.ErrSymlink, reason: skipFinalSymlink},
		{match: filesnapshot.ErrNotRegular, reason: skipNotRegular},
		{match: filesnapshot.ErrLimitExceeded, reason: skipTooLarge},
		{match: filesnapshot.ErrChanged, reason: skipChangedDuringRead},
	} {
		if errors.Is(err, candidate.match) {
			return adopt.Skipped{LivePath: livePath, Reason: candidate.reason}, true
		}
	}
	return adopt.Skipped{}, false
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
	case mcpcodec.MCPProjectionReasonDuplicateKey:
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
