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
	var importConfig func(context.Context, []byte, *mcpProjectionAdmission) error
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
	admission := &mcpProjectionAdmission{
		ctx:          ctx,
		target:       target,
		scope:        scope,
		source:       source,
		document:     document,
		maximumBytes: maximumBytes,
		skipped:      skipped,
		before:       hooks.beforeArgumentAdmission,
	}
	if err := importConfig(ctx, document.content, admission); err != nil {
		if err := classifyMCPExtractionError(ctx, skipped, source.primaryPath, err); err != nil {
			return nil, nil, err
		}
	}
	return finishCandidates(ctx, hooks, admission.servers, authorities)
}

type mcpProjectionAdmission struct {
	ctx             context.Context
	target          targetpkg.Target
	scope           targetpkg.Scope
	source          importSource
	document        importDocument
	maximumBytes    int64
	skipped         adopt.SkipEmitter
	before          func(int)
	projectionIndex int
	servers         []adopt.MCPServer
}

func (admission *mcpProjectionAdmission) admit(
	contentPath string,
	resourceName string,
	command string,
	args []string,
	environment func() map[string]string,
) error {
	index := admission.projectionIndex
	admission.projectionIndex++
	if admission.before != nil {
		admission.before(index)
	}
	if err := mcpImportContextError(admission.ctx); err != nil {
		return &mcpImportSinkError{cause: err}
	}
	route, err := admission.source.route(contentPath, admission.document, admission.maximumBytes)
	if err != nil {
		return &mcpImportSinkError{cause: err}
	}
	server := adopt.MCPServer{
		ResourceName: resourceName,
		Target:       admission.target,
		Scope:        admission.scope,
		SourceRoute:  route,
		Command:      command,
		Args:         args,
	}
	validationErr := desiredmcp.ValidateStdioArguments(args)
	if err := mcpImportContextError(admission.ctx); err != nil {
		return &mcpImportSinkError{cause: err}
	}
	if validationErr != nil {
		if err := admission.skipped.Add(adopt.Skipped{
			LivePath: server.LivePath(),
			Reason:   skipInvalidArgument,
		}); err != nil {
			return &mcpImportSinkError{cause: err}
		}
		return nil
	}
	if environment == nil {
		environment = func() map[string]string { return map[string]string{} }
	}
	server.Env = environment()
	admission.servers = append(admission.servers, server)
	return nil
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

func claudeProjectCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractClaudeProjectMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.ClaudeProjectMCPServerProjection) error {
			return admission.admit(
				mcpcodec.ClaudeProjectMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				func() map[string]string { return hostEnvReferences(projection.Env) },
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

func claudeGlobalCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractClaudeGlobalMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.ClaudeGlobalMCPServerProjection) error {
			return admission.admit(
				mcpcodec.ClaudeGlobalMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				func() map[string]string { return hostEnvReferences(projection.Env) },
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

func openCodeProjectCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractOpenCodeProjectMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.MCPNoEnvServerProjection) error {
			return admission.admit(
				mcpcodec.OpenCodeProjectMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				nil,
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

func openCodeGlobalCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractOpenCodeGlobalMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.OpenCodeGlobalMCPServerProjection) error {
			return admission.admit(
				mcpcodec.OpenCodeGlobalMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				func() map[string]string { return openCodeEnvReferences(projection.Environment) },
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

func codexProjectCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractCodexProjectMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.MCPNoEnvServerProjection) error {
			return admission.admit(
				mcpcodec.CodexProjectMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				nil,
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

func codexGlobalCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractCodexGlobalMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.CodexGlobalMCPServerProjection) error {
			return admission.admit(
				mcpcodec.CodexGlobalMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				func() map[string]string { return sameNameEnvReferences(projection.EnvVars) },
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

func antigravityGlobalCandidates(ctx context.Context, content []byte, admission *mcpProjectionAdmission) error {
	return mcpcodec.ExtractAntigravityGlobalMCPServerProjections(
		ctx,
		content,
		func(projection mcpcodec.AntigravityGlobalMCPServerProjection) error {
			return admission.admit(
				mcpcodec.AntigravityGlobalMCPContentPath(projection.ServerID),
				projection.ServerID,
				projection.Command,
				projection.Args,
				nil,
			)
		},
		mcpRejectionSink(ctx, admission.skipped, admission.source.primaryPath),
	)
}

type mcpImportSinkError struct {
	cause error
}

func (err *mcpImportSinkError) Error() string {
	return err.cause.Error()
}

func (err *mcpImportSinkError) Unwrap() error {
	return err.cause
}

func mcpRejectionSink(
	ctx context.Context,
	skipped adopt.SkipEmitter,
	livePath string,
) mcpcodec.MCPProjectionRejectionSink {
	return func(rejection mcpcodec.MCPProjectionRejection) error {
		if err := mcpImportContextError(ctx); err != nil {
			return &mcpImportSinkError{cause: err}
		}
		if err := skipped.Add(adopt.Skipped{
			LivePath: livePath + "#" + string(rejection.ContentPath()),
			Reason:   reasonString(rejection.Reason()),
		}); err != nil {
			return &mcpImportSinkError{cause: err}
		}
		return nil
	}
}

func classifyMCPExtractionError(
	ctx context.Context,
	skipped adopt.SkipEmitter,
	livePath string,
	err error,
) error {
	var emissionError *mcpImportSinkError
	if errors.As(err, &emissionError) {
		return emissionError.cause
	}
	if contextErr := mcpImportContextError(ctx); contextErr != nil {
		return contextErr
	}
	return skipped.Add(adopt.Skipped{
		LivePath: livePath,
		Reason:   skipReason(err),
	})
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
