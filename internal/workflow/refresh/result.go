package refresh

import (
	"fmt"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
	"github.com/isty2e/daem/internal/subprocess"
)

const unavailableFailureDetail = "refresh failed; no additional public detail is available"

// RefusalError is a stable pre-attempt workflow refusal.
type RefusalError struct {
	code  ReasonCode
	cause error
}

func (err *RefusalError) Error() string {
	if err == nil {
		return ""
	}
	if err.cause == nil {
		return string(err.code)
	}
	return fmt.Sprintf("%s: %s", err.code, err.cause)
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
	result.RecoveryBarrier = recoverygate.StateOf(cause)
	if remediation != "" {
		result.Remediation = []string{remediation}
	}
	return result, &RefusalError{code: code, cause: cause}
}

// FailureDetail derives public prose exclusively from closed workflow facts.
// External error strings remain internal causes and never participate in this
// projection.
func (result CommandResult) FailureDetail() string {
	if !result.HasErrors() {
		return ""
	}
	return result.failureDetail() + result.recoveryBarrierDetail()
}

func (result CommandResult) failureDetail() string {
	switch result.ReasonCode {
	case ReasonInvalidSelection:
		return "the selected extension relation is invalid"
	case ReasonManifestUnavailable:
		return "the selected manifest is unavailable"
	case ReasonLockUnavailable:
		return "the selected lockfile is unavailable"
	case ReasonLockMismatch:
		return "the selected lockfile does not match the manifest"
	case ReasonRefreshUnsupported:
		return "the selected relation has no supported refresh route"
	case ReasonRelationMissing:
		return "the selected relation is missing from current host state"
	case ReasonRelationAmbiguous:
		return "the selected relation is ambiguous in current host state"
	case ReasonObservationUnavailable:
		return "required current relation evidence is unavailable"
	case ReasonStalePlan:
		return "the authorized refresh plan is stale"
	case ReasonMutationAuthority:
		return "required mutation authority is unavailable"
	case ReasonInterruptedApply:
		return "interrupted apply journal is present; run daem recover first"
	case ReasonInterruptedApplyFileSetFence:
		return "interrupted apply journal is present; run daem recover first; the file-set fence remains after recover"
	case ReasonJournalCleanupIncomplete:
		return "journal cleanup is incomplete; run daem recover first"
	case ReasonJournalCleanupFileSetFence:
		return "journal cleanup is incomplete; run daem recover first; the file-set fence remains after recover"
	case ReasonInterruptedFileSetTransaction:
		return "an interrupted file-set transaction requires its owning workflow to recover it"
	case ReasonFileSetEvidenceInvalid:
		return "file-set transaction evidence is incomplete or invalid"
	case ReasonAbandonedFileSetResidue:
		return "abandoned file-set residue remains; current daem cannot recover markerless residue"
	case ReasonFileSetFenceCensusLimit:
		return "the bounded file-set fence census could not prove the fence clean"
	case ReasonFileSetAccessUnprovable:
		return "file-set state directory access or identity could not be proven"
	case ReasonCommandFailed:
		return result.commandFailureDetail()
	case ReasonInvalidTimeout:
		return "the refresh timeout is invalid"
	case ReasonPostObservationFailed:
		return result.postObservationFailureDetail()
	case ReasonAttemptPersistence:
		return "refresh attempt history could not be persisted"
	case ReasonCancelled:
		if detail, ok := result.typedCommandFailureDetail(); ok {
			return detail
		}
		return "refresh was cancelled"
	case ReasonNone:
		return unavailableFailureDetail
	default:
		return unavailableFailureDetail
	}
}

func (result CommandResult) recoveryBarrierDetail() string {
	detail := ""
	if result.RecoveryBarrier.JournalObserved() && !result.RecoveryBarrier.JournalKnown() {
		detail += "; journal recovery authority could not be classified"
	}
	if result.RecoveryBarrier.FileSetObserved() && !result.RecoveryBarrier.FileSetKnown() {
		detail += "; file-set fence could not be classified"
	}
	return detail
}

func (result CommandResult) commandFailureDetail() string {
	if detail, ok := result.typedCommandFailureDetail(); ok {
		return detail
	}
	return "the delegated host command failed"
}

func (result CommandResult) typedCommandFailureDetail() (string, bool) {
	if result.ProcessOutcome == nil {
		return "", false
	}
	reason, ok := publicCommandReason(result.ProcessOutcome.Reason)
	if !ok || reason == "" {
		return "", false
	}
	return "delegated host command result: " + reason, true
}

func publicCommandReason(reason subprocess.CommandReason) (string, bool) {
	switch reason {
	case subprocess.CommandReasonNone:
		return "", true
	case subprocess.CommandReasonMissingEnvRef:
		return "missing_env_ref", true
	case subprocess.CommandReasonMissingRunner:
		return "missing_runner", true
	case subprocess.CommandReasonNonZeroExit:
		return "nonzero_exit", true
	case subprocess.CommandReasonTimeout:
		return "timeout", true
	case subprocess.CommandReasonCanceled:
		return "canceled", true
	case subprocess.CommandReasonSignaled:
		return "signaled", true
	case subprocess.CommandReasonRunnerError:
		return "runner_error", true
	default:
		return "", false
	}
}

func (result CommandResult) postObservationFailureDetail() string {
	if result.Observation == nil {
		return "post-attempt relation observation did not satisfy the refresh postcondition"
	}
	state, stateOK := publicObservationState(result.Observation.State)
	reason, reasonOK := publicObservationReason(result.Observation.Reason)
	if !stateOK || !reasonOK {
		return "post-attempt relation observation did not satisfy the refresh postcondition"
	}
	if reason == "" {
		return "post-attempt relation observation: state=" + state
	}
	return "post-attempt relation observation: state=" + state + " reason=" + reason
}

func publicObservationState(state observerelation.CorrelationState) (string, bool) {
	switch state {
	case observerelation.StateExactCorrelation:
		return "exact_correlation", true
	case observerelation.StateMissing:
		return "missing", true
	case observerelation.StateUnkeyedSameSubject:
		return "unkeyed_same_subject", true
	case observerelation.StateSameSubjectShadow:
		return "same_name_shadow", true
	case observerelation.StateManagedKeyDrift:
		return "managed_key_drift", true
	case observerelation.StateAmbiguous:
		return "ambiguous", true
	case observerelation.StateStaleEvidence:
		return "stale_evidence", true
	case observerelation.StateUnsupported:
		return "unsupported", true
	case observerelation.StateUnavailableEvidence:
		return "unavailable_evidence", true
	default:
		return "", false
	}
}

func publicObservationReason(reason observerelation.ReasonCode) (string, bool) {
	switch reason {
	case observerelation.ReasonNone:
		return "", true
	case observerelation.ReasonUnsupportedInventory:
		return "unsupported_passive_inventory", true
	case observerelation.ReasonStaleEvidence:
		return "stale_evidence", true
	case observerelation.ReasonMissing:
		return "managed_relation_missing", true
	case observerelation.ReasonUnkeyedSameSubject:
		return "unkeyed_same_subject", true
	case observerelation.ReasonSameSubjectShadow:
		return "same_name_shadow", true
	case observerelation.ReasonManagedKeyDrift:
		return "managed_key_drift", true
	case observerelation.ReasonAmbiguous:
		return "ambiguous_relation", true
	case observerelation.ReasonUnavailableEvidence:
		return "relation_evidence_unavailable", true
	default:
		return "", false
	}
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
