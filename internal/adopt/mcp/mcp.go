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

// Candidates observes one MCP document, emits classified skips at their first
// amplifying boundary, and returns admitted standalone projections plus exact
// source authority.
func Candidates(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	skipped adopt.SkipEmitter,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, error) {
	return candidatesWithHooks(ctx, target, scope, skipped, candidateHooks{})
}

func candidatesWithHooks(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	skipped adopt.SkipEmitter,
	hooks candidateHooks,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("MCP import context is required")
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, err
	}
	var importConfig func(context.Context, importSource, importDocument, int64, adopt.SkipEmitter) ([]adopt.MCPServer, error)
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
		if err := skipped.Add(adopt.UnsupportedSurfaceSkip(target, scope, "mcp_server")); err != nil {
			return nil, nil, err
		}
		return finishCandidates(ctx, hooks, nil, nil)
	}
	placement, ok := aggregate.ImplementedMCPPlacement(target, scope)
	if !ok {
		return nil, nil, fmt.Errorf("MCP import route %s/%s has no canonical placement", target, scope)
	}
	codec, ok := aggregatecodec.Catalog().Lookup(placement.CodecContractID())
	if !ok {
		return nil, nil, fmt.Errorf("MCP import route %s/%s has no aggregate codec", target, scope)
	}
	primaryPath, err := mcpConfigPath(placement.ConfigPath(), scope)
	if err != nil {
		return nil, nil, err
	}
	requiredAbsentPaths := make([]string, 0, 1)
	if conflictingConfig, hasConflict := placement.ConflictingConfigPath(); hasConflict {
		conflictingPath, err := mcpConfigPath(conflictingConfig, scope)
		if err != nil {
			return nil, nil, err
		}
		requiredAbsentPaths = append(requiredAbsentPaths, conflictingPath)
	}
	source := newImportSource(primaryPath, requiredAbsentPaths...)
	maximumBytes := codec.MaximumDocumentBytes()
	document, skip, err := readImportSource(ctx, source, maximumBytes)
	if err != nil || skip.Reason != "" {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		if err != nil {
			return nil, nil, err
		}
		if err := skipped.Add(skip); err != nil {
			return nil, nil, err
		}
		return finishCandidates(ctx, hooks, nil, nil)
	}
	if hooks.afterSnapshot != nil {
		hooks.afterSnapshot()
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, err
	}
	authorities := []adopt.MCPSourceAuthority{
		source.authority(target, scope, document, maximumBytes),
	}
	servers, err := importConfig(ctx, source, document, maximumBytes, skipped)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, nil, contextErr
		}
		return nil, nil, err
	}
	servers, err = admitMCPArgumentCandidates(ctx, servers, skipped, hooks.beforeArgumentAdmission)
	if err != nil {
		return nil, nil, err
	}
	return finishCandidates(ctx, hooks, servers, authorities)
}

func admitMCPArgumentCandidates(
	ctx context.Context,
	servers []adopt.MCPServer,
	skipped adopt.SkipEmitter,
	before func(int),
) ([]adopt.MCPServer, error) {
	admitted := make([]adopt.MCPServer, 0, len(servers))
	for index, server := range servers {
		if before != nil {
			before(index)
		}
		if err := mcpImportContextError(ctx); err != nil {
			return nil, err
		}
		validationErr := desiredmcp.ValidateStdioArguments(server.Args)
		if err := mcpImportContextError(ctx); err != nil {
			return nil, err
		}
		if validationErr != nil {
			if err := skipped.Add(adopt.Skipped{
				LivePath: server.LivePath(),
				Reason:   skipInvalidArgument,
			}); err != nil {
				return nil, err
			}
			continue
		}
		admitted = append(admitted, server)
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, err
	}
	return admitted, nil
}

func finishCandidates(
	ctx context.Context,
	hooks candidateHooks,
	servers []adopt.MCPServer,
	authorities []adopt.MCPSourceAuthority,
) ([]adopt.MCPServer, []adopt.MCPSourceAuthority, error) {
	if hooks.beforeSuccess != nil {
		hooks.beforeSuccess()
	}
	if err := mcpImportContextError(ctx); err != nil {
		return nil, nil, err
	}
	return servers, authorities, nil
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

func claudeProjectCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractClaudeProjectMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.ClaudeProjectMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func claudeGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractClaudeGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.ClaudeGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func openCodeProjectCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractOpenCodeProjectMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.OpenCodeProjectMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func openCodeGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractOpenCodeGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.OpenCodeGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func codexProjectCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractCodexProjectMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.CodexProjectMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func codexGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractCodexGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.CodexGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func antigravityGlobalCandidates(ctx context.Context, source importSource, document importDocument, maximumBytes int64, skipped adopt.SkipEmitter) ([]adopt.MCPServer, error) {
	projections, rejections, err := mcpcodec.ExtractAntigravityGlobalMCPServerProjections(ctx, document.content)
	if err != nil {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		if err := skipped.Add(adopt.Skipped{LivePath: source.primaryPath, Reason: skipReason(err)}); err != nil {
			return nil, err
		}
		return nil, nil
	}
	servers := make([]adopt.MCPServer, 0, len(projections))
	for _, projection := range projections {
		if contextErr := mcpImportContextError(ctx); contextErr != nil {
			return nil, contextErr
		}
		route, err := source.route(mcpcodec.AntigravityGlobalMCPContentPath(projection.ServerID), document, maximumBytes)
		if err != nil {
			return nil, err
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
	if err := emitMCPRejections(ctx, skipped, source.primaryPath, rejections); err != nil {
		return nil, err
	}
	return servers, mcpImportContextError(ctx)
}

func emitMCPRejections(
	ctx context.Context,
	skipped adopt.SkipEmitter,
	livePath string,
	rejections []mcpcodec.MCPProjectionRejection,
) error {
	for _, rejection := range rejections {
		if err := mcpImportContextError(ctx); err != nil {
			return err
		}
		if err := skipped.Add(adopt.Skipped{
			LivePath: livePath + "#" + string(rejection.ContentPath()),
			Reason:   reasonString(rejection.Reason()),
		}); err != nil {
			return err
		}
	}
	return mcpImportContextError(ctx)
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
		reason adopt.SkipReason
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

func skipReason(err error) adopt.SkipReason {
	reason, ok := mcpcodec.MCPProjectionReasonCodeOf(err)
	if !ok {
		return "mcp_projection_unclassified"
	}
	return reasonString(reason)
}

func reasonString(reason mcpcodec.MCPProjectionReasonCode) adopt.SkipReason {
	switch reason {
	case mcpcodec.MCPProjectionReasonConfigMalformed:
		return "mcp_config_malformed"
	case mcpcodec.MCPProjectionReasonDuplicateKey:
		return "duplicate_json_key"
	case mcpcodec.MCPProjectionReasonUnsupportedTransport:
		return "unsupported_mcp_transport"
	case mcpcodec.MCPProjectionReasonUnsupportedManagedField:
		return "unsupported_mcp_managed_field"
	case mcpcodec.MCPProjectionReasonSecretLiteralForbidden:
		return "secret_literal_forbidden"
	case mcpcodec.MCPProjectionReasonProjectionEquivalenceUndefined:
		return "projection_equivalence_undefined"
	case mcpcodec.MCPProjectionReasonCanonicalInvalid:
		return "invalid_canonical_mcp"
	case mcpcodec.MCPProjectionReasonStaleAdapterContract:
		return "stale_adapter_contract"
	case mcpcodec.MCPProjectionReasonProviderDocumentLossy:
		return "mcp_provider_document_lossy"
	default:
		return "mcp_projection_unclassified"
	}
}
