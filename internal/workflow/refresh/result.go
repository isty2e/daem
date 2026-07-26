package refresh

import (
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
)

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
	if remediation != "" {
		result.Remediation = []string{remediation}
	}
	return result, &RefusalError{code: code, detail: cause.Error(), cause: cause}
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
