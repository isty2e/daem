package mcpcodec

func compareClaudeProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeClaudeProjectMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareClaudeProjectMCPServerEntry(existing, serverID, desired)
}

func compareClaudeGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeClaudeGlobalMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareClaudeGlobalMCPServerEntry(existing, serverID, desired)
}

func compareAntigravityGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeAntigravityGlobalMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareAntigravityGlobalMCPServerEntry(existing, serverID, desired)
}

func compareOpenCodeProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeOpenCodeProjectMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareOpenCodeProjectMCPServerEntry(existing, serverID, desired)
}

func compareOpenCodeGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeOpenCodeProjectMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareOpenCodeGlobalMCPServerEntry(existing, serverID, desired)
}

func compareCodexProjectMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeCodexProjectMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareCodexProjectMCPServerEntry(existing, serverID, desired)
}

func compareCodexGlobalMCPServerCanonicalEntry(existing []byte, serverID string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
	desired, err := decodeCodexProjectMCPServerEntry(canonical, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	return compareCodexGlobalMCPServerEntry(existing, serverID, desired)
}

func compareClaudeProjectMCPServerEntry(existing []byte, serverID string, desired ClaudeProjectMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractClaudeProjectMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: ClaudeProjectMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = mcpServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func compareClaudeGlobalMCPServerEntry(existing []byte, serverID string, desired ClaudeGlobalMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractClaudeGlobalMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: ClaudeGlobalMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = claudeGlobalMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func compareAntigravityGlobalMCPServerEntry(existing []byte, serverID string, desired AntigravityGlobalMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractAntigravityGlobalMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: AntigravityGlobalMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = antigravityGlobalMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func compareOpenCodeProjectMCPServerEntry(existing []byte, serverID string, desired OpenCodeMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractOpenCodeProjectMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: OpenCodeProjectMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = openCodeProjectMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func compareOpenCodeGlobalMCPServerEntry(existing []byte, serverID string, desired OpenCodeMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractOpenCodeGlobalMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: OpenCodeGlobalMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = openCodeProjectMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func compareCodexProjectMCPServerEntry(existing []byte, serverID string, desired CodexMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractCodexProjectMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: CodexProjectMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = codexProjectMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func compareCodexGlobalMCPServerEntry(existing []byte, serverID string, desired CodexMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
	existingEntry, ok, err := ExtractCodexGlobalMCPServerProjection(existing, serverID)
	if err != nil {
		return MCPProjectionCanonicalComparison{}, err
	}
	comparison := MCPProjectionCanonicalComparison{
		ContentPath: CodexGlobalMCPContentPath(serverID),
		Present:     ok,
	}
	if !ok {
		return comparison, nil
	}
	comparison.Equivalent = codexProjectMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func mcpServerEntriesEqual(left ClaudeProjectMCPServerEntry, right ClaudeProjectMCPServerEntry) bool {
	if left.Type != right.Type || left.Command != right.Command {
		return false
	}
	if len(left.Args) != len(right.Args) {
		return false
	}
	for index := range left.Args {
		if left.Args[index] != right.Args[index] {
			return false
		}
	}
	if len(left.Env) != len(right.Env) {
		return false
	}
	for key, leftValue := range left.Env {
		if right.Env[key] != leftValue {
			return false
		}
	}
	return true
}

func claudeGlobalMCPServerEntriesEqual(left ClaudeGlobalMCPServerEntry, right ClaudeGlobalMCPServerEntry) bool {
	if left.Type != right.Type || left.Command != right.Command {
		return false
	}
	if len(left.Args) != len(right.Args) {
		return false
	}
	for index := range left.Args {
		if left.Args[index] != right.Args[index] {
			return false
		}
	}
	return len(left.Env) == 0 && len(right.Env) == 0
}

func codexProjectMCPServerEntriesEqual(left CodexMCPServerEntry, right CodexMCPServerEntry) bool {
	if left.Command != right.Command || len(left.Args) != len(right.Args) {
		return false
	}
	for index := range left.Args {
		if left.Args[index] != right.Args[index] {
			return false
		}
	}
	return true
}

func antigravityGlobalMCPServerEntriesEqual(left AntigravityGlobalMCPServerEntry, right AntigravityGlobalMCPServerEntry) bool {
	if left.Command != right.Command {
		return false
	}
	if len(left.Args) != len(right.Args) {
		return false
	}
	for index := range left.Args {
		if left.Args[index] != right.Args[index] {
			return false
		}
	}
	return true
}

func openCodeProjectMCPServerEntriesEqual(left OpenCodeMCPServerEntry, right OpenCodeMCPServerEntry) bool {
	if left.Type != right.Type || len(left.Command) != len(right.Command) {
		return false
	}
	for index := range left.Command {
		if left.Command[index] != right.Command[index] {
			return false
		}
	}
	return true
}
