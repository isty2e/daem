package refresh

import (
	"fmt"
	"slices"
	"strings"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/subprocess"
)

const maximumFailureDetailRunes = 1024

const failureDetailCaptureRunes = maximumFailureDetailRunes - len("\n[truncated]")

// RefusalError is a stable pre-attempt workflow refusal.
type RefusalError struct {
	code   ReasonCode
	detail string
	cause  error
}

func (err *RefusalError) Error() string {
	if err == nil {
		return ""
	}
	if err.detail == "" {
		return string(err.code)
	}
	return fmt.Sprintf("%s: %s", err.code, err.detail)
}

func (err *RefusalError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *RefusalError) Code() ReasonCode {
	if err == nil {
		return ""
	}
	return err.code
}

func baseResult(paths daempaths.Paths, mode Mode) CommandResult {
	return CommandResult{
		Mode:          mode,
		ManifestPath:  paths.ManifestPath,
		LockfilePath:  paths.LockfilePath,
		StatefilePath: paths.StatefilePath,
	}
}

func refusedPlan(
	result CommandResult,
	code ReasonCode,
	cause error,
	remediation string,
) (plan, error) {
	refused, err := refusedResult(result, code, cause, remediation)
	return plan{result: refused}, err
}

func refusedResult(
	result CommandResult,
	code ReasonCode,
	cause error,
	remediation string,
) (CommandResult, error) {
	result.ResultClass = ResultRefused
	if code == ReasonCancelled {
		result.ResultClass = ResultCancelled
	}
	result.ReasonCode = code
	result.failureDetail = sanitizedFailureDetail(cause)
	if remediation != "" {
		result.Remediation = []string{remediation}
	}
	return result, &RefusalError{code: code, detail: cause.Error(), cause: cause}
}

func withFailureDetail(result CommandResult, cause error) CommandResult {
	result.failureDetail = sanitizedFailureDetail(cause)
	return result
}

func sanitizedFailureDetail(cause error) string {
	if cause == nil {
		return ""
	}
	sanitized := subprocess.NewCapturePolicy(
		nil,
		failureDetailCaptureRunes,
	).Sanitize(cause.Error(), false).Text()
	redacted := redactMachineLocalPaths(sanitized)
	return subprocess.NewCapturePolicy(
		nil,
		failureDetailCaptureRunes,
	).Sanitize(redacted, false).Text()
}

func redactMachineLocalPaths(value string) string {
	var result strings.Builder
	result.Grow(len(value))
	for index := 0; index < len(value); {
		if !machineLocalPathStartsAt(value, index) {
			result.WriteByte(value[index])
			index++
			continue
		}
		result.WriteString("[REDACTED]")
		index = machineLocalPathEnd(value, index)
	}
	return result.String()
}

func machineLocalPathStartsAt(value string, index int) bool {
	remaining := value[index:]
	switch {
	case hasPrefixFold(remaining, "file:"):
		return pathTokenBoundary(value, index)
	case hasPrefixFold(remaining, "local:"):
		return pathTokenBoundary(value, index)
	case strings.HasPrefix(remaining, "~/"), strings.HasPrefix(remaining, `~\`),
		strings.HasPrefix(remaining, "./"), strings.HasPrefix(remaining, "../"),
		strings.HasPrefix(remaining, `.\`), strings.HasPrefix(remaining, `..\`),
		strings.HasPrefix(remaining, `\\`):
		return pathTokenBoundary(value, index)
	case remaining[0] == '/':
		return (index == 0 || index+1 >= len(value) ||
			value[index-1] != ':' || value[index+1] != '/') &&
			pathTokenBoundary(value, index)
	case len(remaining) >= 3 && isASCIILetter(remaining[0]) &&
		remaining[1] == ':' && (remaining[2] == '/' || remaining[2] == '\\'):
		return pathTokenBoundary(value, index)
	default:
		return false
	}
}

func pathTokenBoundary(value string, index int) bool {
	if index == 0 {
		return true
	}
	previous := value[index-1]
	return previous == ' ' || previous == '\t' || previous == '\n' ||
		previous == '\r' || previous == '"' || previous == '\'' ||
		previous == '(' || previous == '[' || previous == '{' ||
		previous == '=' || previous == ':'
}

func machineLocalPathEnd(value string, start int) int {
	quote := byte(0)
	if start > 0 && (value[start-1] == '"' || value[start-1] == '\'') {
		quote = value[start-1]
	}
	for index := start; index < len(value); index++ {
		if quote != 0 {
			if value[index] == quote && (index == start || value[index-1] != '\\') {
				return index
			}
			continue
		}
		if value[index] == ':' && index-start > 1 &&
			index+1 < len(value) && isHorizontalSpace(value[index+1]) {
			return index + 1
		}
		if isPathEndTerminator(value[index]) {
			return index
		}
	}
	return len(value)
}

func isPathEndTerminator(value byte) bool {
	switch value {
	case '\n', '\r':
		return true
	default:
		return false
	}
}

func isHorizontalSpace(value byte) bool {
	return value == ' ' || value == '\t'
}

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func hasPrefixFold(value string, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}

func observationSummary(result observerelation.CorrelationResult) *Observation {
	return &Observation{
		State:        result.State(),
		Reason:       result.Reason(),
		Availability: result.EvidenceAvailability(),
		Freshness:    result.EvidenceFreshness(),
	}
}

func cloneObservation(value *Observation) *Observation {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneCommandResult(result CommandResult) CommandResult {
	cloned := result
	cloned.Disclosure.Invocation.Args = append([]string(nil), result.Disclosure.Invocation.Args...)
	cloned.Disclosure.Invocation.EnvNames = append([]string(nil), result.Disclosure.Invocation.EnvNames...)
	cloned.Disclosure.EffectClasses = append([]string(nil), result.Disclosure.EffectClasses...)
	cloned.Disclosure.RetainedEffectClasses = append([]string(nil), result.Disclosure.RetainedEffectClasses...)
	cloned.Disclosure.NonClaims = append([]string(nil), result.Disclosure.NonClaims...)
	cloned.Observation = cloneObservation(result.Observation)
	if result.ProcessOutcome != nil {
		outcome := *result.ProcessOutcome
		if result.ProcessOutcome.ExitCode != nil {
			exitCode := *result.ProcessOutcome.ExitCode
			outcome.ExitCode = &exitCode
		}
		cloned.ProcessOutcome = &outcome
	}
	cloned.Remediation = append([]string(nil), result.Remediation...)
	return cloned
}

func canonicalObservationAuthorityPaths(
	paths []observerelation.AuthorityPath,
) ([]observerelation.AuthorityPath, error) {
	byKey := make(map[string]observerelation.AuthorityPath, len(paths))
	for index, path := range paths {
		canonical, err := observerelation.NewAuthorityPath(
			path.Path(),
			path.Target(),
			path.Scope(),
		)
		if err != nil {
			return nil, fmt.Errorf("observation authority path[%d]: %w", index, err)
		}
		key := fmt.Sprintf("%s\x00%s\x00%s", canonical.Target(), canonical.Scope(), canonical.Path())
		byKey[key] = canonical
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	canonical := make([]observerelation.AuthorityPath, 0, len(keys))
	for _, key := range keys {
		canonical = append(canonical, byKey[key])
	}
	return canonical, nil
}
