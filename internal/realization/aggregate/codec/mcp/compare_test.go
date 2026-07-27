package mcpcodec

func compareClaudeProjectMCPServerProjection(existing []byte, projection ClaudeProjectMCPServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalClaudeProjectMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareClaudeProjectMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func compareClaudeGlobalMCPServerProjection(existing []byte, projection MCPNoEnvServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalClaudeGlobalMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareClaudeGlobalMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func compareAntigravityGlobalMCPServerProjection(existing []byte, projection MCPNoEnvServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalAntigravityGlobalMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareAntigravityGlobalMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func compareOpenCodeProjectMCPServerProjection(existing []byte, projection MCPNoEnvServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalOpenCodeProjectMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareOpenCodeProjectMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func compareOpenCodeGlobalMCPServerProjection(existing []byte, projection MCPNoEnvServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalOpenCodeGlobalMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareOpenCodeGlobalMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func compareCodexProjectMCPServerProjection(existing []byte, projection MCPNoEnvServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalCodexProjectMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareCodexProjectMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}

func compareCodexGlobalMCPServerProjection(existing []byte, projection MCPNoEnvServerProjection) (MCPProjectionCanonicalComparison, error) {
	canonical, err := CanonicalCodexGlobalMCPServerEntry(projection)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareCodexGlobalMCPServerCanonicalEntry(existing, projection.ServerID, canonical)
}
