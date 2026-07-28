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
	desired, err := decodeCodexGlobalMCPServerEntry(canonical, serverID)
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

func compareCodexProjectMCPServerEntry(existing []byte, serverID string, desired CodexProjectMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
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

func compareCodexGlobalMCPServerEntry(existing []byte, serverID string, desired CodexGlobalMCPServerEntry) (MCPProjectionCanonicalComparison, error) {
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
	comparison.Equivalent = codexGlobalMCPServerEntriesEqual(existingEntry, desired)
	return comparison, nil
}

func mcpServerEntriesEqual(left ClaudeProjectMCPServerEntry, right ClaudeProjectMCPServerEntry) bool {
	return claudeMCPServerFieldsEqual(
		left.Type, left.Command, left.Args, left.Env,
		right.Type, right.Command, right.Args, right.Env,
	)
}

func claudeGlobalMCPServerEntriesEqual(left ClaudeGlobalMCPServerEntry, right ClaudeGlobalMCPServerEntry) bool {
	return claudeMCPServerFieldsEqual(
		left.Type, left.Command, left.Args, left.Env,
		right.Type, right.Command, right.Args, right.Env,
	)
}

func claudeMCPServerFieldsEqual(
	leftType string,
	leftCommand string,
	leftArgs []string,
	leftEnv map[string]string,
	rightType string,
	rightCommand string,
	rightArgs []string,
	rightEnv map[string]string,
) bool {
	if leftType != rightType || leftCommand != rightCommand {
		return false
	}
	if len(leftArgs) != len(rightArgs) {
		return false
	}
	for index := range leftArgs {
		if leftArgs[index] != rightArgs[index] {
			return false
		}
	}
	if len(leftEnv) != len(rightEnv) {
		return false
	}
	for key, leftValue := range leftEnv {
		if rightEnv[key] != leftValue {
			return false
		}
	}
	return true
}

func codexProjectMCPServerEntriesEqual(left CodexProjectMCPServerEntry, right CodexProjectMCPServerEntry) bool {
	return codexMCPCommandArgsEqual(left.Command, left.Args, right.Command, right.Args)
}

func codexGlobalMCPServerEntriesEqual(left CodexGlobalMCPServerEntry, right CodexGlobalMCPServerEntry) bool {
	if !codexMCPCommandArgsEqual(left.Command, left.Args, right.Command, right.Args) ||
		len(left.EnvVars) != len(right.EnvVars) {
		return false
	}
	for index := range left.EnvVars {
		if left.EnvVars[index] != right.EnvVars[index] {
			return false
		}
	}
	return true
}

func codexMCPCommandArgsEqual(
	leftCommand string,
	leftArgs []string,
	rightCommand string,
	rightArgs []string,
) bool {
	if leftCommand != rightCommand || len(leftArgs) != len(rightArgs) {
		return false
	}
	for index := range leftArgs {
		if leftArgs[index] != rightArgs[index] {
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
