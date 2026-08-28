package mcpcodec

import "context"

func collectMCPProjectionRejections[T any](
	ctx context.Context,
	content []byte,
	extract func(context.Context, []byte, MCPProjectionRejectionSink) ([]T, error),
) ([]T, []MCPProjectionRejection, error) {
	rejections := make([]MCPProjectionRejection, 0)
	projections, err := extract(ctx, content, func(rejection MCPProjectionRejection) error {
		rejections = append(rejections, rejection)
		return nil
	})
	return projections, rejections, err
}

func collectClaudeProjectMCPServerProjections(ctx context.Context, content []byte) ([]ClaudeProjectMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractClaudeProjectMCPServerProjections)
}

func collectClaudeGlobalMCPServerProjections(ctx context.Context, content []byte) ([]ClaudeGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractClaudeGlobalMCPServerProjections)
}

func collectOpenCodeProjectMCPServerProjections(ctx context.Context, content []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractOpenCodeProjectMCPServerProjections)
}

func collectOpenCodeGlobalMCPServerProjections(ctx context.Context, content []byte) ([]OpenCodeGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractOpenCodeGlobalMCPServerProjections)
}

func collectCodexProjectMCPServerProjections(ctx context.Context, content []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractCodexProjectMCPServerProjections)
}

func collectCodexGlobalMCPServerProjections(ctx context.Context, content []byte) ([]CodexGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractCodexGlobalMCPServerProjections)
}

func collectAntigravityGlobalMCPServerProjections(ctx context.Context, content []byte) ([]AntigravityGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionRejections(ctx, content, ExtractAntigravityGlobalMCPServerProjections)
}
