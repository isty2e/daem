package mcpcodec

import "context"

func collectMCPProjectionClassifications[T any](
	ctx context.Context,
	content []byte,
	extract func(
		context.Context,
		[]byte,
		MCPProjectionSink[T],
		MCPProjectionRejectionSink,
	) error,
) ([]T, []MCPProjectionRejection, error) {
	projections := make([]T, 0)
	rejections := make([]MCPProjectionRejection, 0)
	err := extract(
		ctx,
		content,
		func(projection T) error {
			projections = append(projections, projection)
			return nil
		},
		func(rejection MCPProjectionRejection) error {
			rejections = append(rejections, rejection)
			return nil
		},
	)
	return projections, rejections, err
}

func collectClaudeProjectMCPServerProjections(ctx context.Context, content []byte) ([]ClaudeProjectMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractClaudeProjectMCPServerProjections)
}

func collectClaudeGlobalMCPServerProjections(ctx context.Context, content []byte) ([]ClaudeGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractClaudeGlobalMCPServerProjections)
}

func collectOpenCodeProjectMCPServerProjections(ctx context.Context, content []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractOpenCodeProjectMCPServerProjections)
}

func collectOpenCodeGlobalMCPServerProjections(ctx context.Context, content []byte) ([]OpenCodeGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractOpenCodeGlobalMCPServerProjections)
}

func collectCodexProjectMCPServerProjections(ctx context.Context, content []byte) ([]MCPNoEnvServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractCodexProjectMCPServerProjections)
}

func collectCodexGlobalMCPServerProjections(ctx context.Context, content []byte) ([]CodexGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractCodexGlobalMCPServerProjections)
}

func collectAntigravityGlobalMCPServerProjections(ctx context.Context, content []byte) ([]AntigravityGlobalMCPServerProjection, []MCPProjectionRejection, error) {
	return collectMCPProjectionClassifications(ctx, content, ExtractAntigravityGlobalMCPServerProjections)
}
