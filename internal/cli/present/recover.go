package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
)

func PrintRecoverPlanWithOptions(
	output io.Writer,
	disclosure journal.RecoverablePlan,
	options HumanOptions,
) {
	if plan, ok := journal.ActiveRecoveryPlan(disclosure); ok {
		printActiveRecoverPlan(output, plan, options)
		return
	}
	if plan, ok := journal.JournalCleanupPlan(disclosure); ok {
		fmt.Fprintf(output, "recover: %s\n", plan.Classification())
		fmt.Fprintf(output, "operation: %s\n", plan.Authority().OperationID())
		fmt.Fprintln(output, plan.Action())
		fmt.Fprintln(output, "journal cleanup only; host, statefile, and ownership data are unchanged")
		return
	}
	fmt.Fprintln(output, "recover: invalid")
}

func printActiveRecoverPlan(
	output io.Writer,
	plan recovery.Plan,
	options HumanOptions,
) {
	fmt.Fprintf(output, "recover: %s\n", plan.Classification())
	if len(plan.Actions()) == 0 && len(plan.RemovalCleanupObligations()) == 0 {
		fmt.Fprintln(output, "nothing to recover")
		return
	}
	if plan.OperationID() != "" {
		fmt.Fprintf(output, "operation: %s\n", plan.OperationID())
	}
	if options.Verbose {
		fmt.Fprintf(output, "operation directory: %s\n", Escape(plan.OperationDir()))
	}
	for _, action := range plan.Actions() {
		identityLabel, identityValue := recoveryIdentityLabel(action)
		if identityValue == "" {
			fmt.Fprintf(output, "%s reason=%s", action.Kind, action.Reason)
		} else {
			targetLabel, targetValue := recoveryTargetLabel(action)
			fmt.Fprintf(
				output,
				"%s %s=%q %s=%s scope=%s destination=%q reason=%s",
				action.Kind,
				identityLabel,
				identityValue,
				targetLabel,
				targetValue,
				action.Scope,
				action.Destination,
				action.Reason,
			)
		}
		if options.Verbose && action.BackupPath != "" {
			fmt.Fprintf(output, " backup=%q", action.BackupPath)
		}
		if action.ContentPath != "" {
			fmt.Fprintf(output, " content_path=%q", action.ContentPath)
		}
		if action.Detail != "" && (options.Verbose || action.Kind == recovery.ActionKindError) {
			fmt.Fprintf(output, " detail=%q", action.Detail)
		}
		fmt.Fprintln(output)
	}
	for _, obligation := range plan.RemovalCleanupObligations() {
		fmt.Fprintf(
			output,
			"removal_cleanup readiness=%s scope=%s destination=%q",
			obligation.Readiness(),
			obligation.Scope(),
			obligation.Destination(),
		)
		if obligation.Action() != "" {
			fmt.Fprintf(output, " action=%s", obligation.Action())
		}
		if obligation.Reason() != recovery.RemovalCleanupReasonNone {
			fmt.Fprintf(output, " reason=%s", obligation.Reason())
		}
		if obligation.Detail() != "" {
			fmt.Fprintf(output, " detail=%q", obligation.Detail())
		}
		fmt.Fprintln(output)
	}
}

func recoveryIdentityLabel(action recovery.Action) (string, string) {
	subject, hasSubject := action.SubjectID()
	if entityID, ok := topologyprojection.EntityID(subject); hasSubject && ok {
		return "resource", string(entityID.Kind()) + "/" + entityID.Name()
	}
	if hasSubject {
		return "subject", subjectString(*planJSONSubjectFor(subject))
	}
	return "", ""
}

func recoveryTargetLabel(action recovery.Action) (string, string) {
	if action.Target != "" {
		return "target", string(action.Target)
	}
	values := make([]string, 0, len(action.ConsumerTargets))
	for _, value := range action.ConsumerTargets {
		values = append(values, string(value))
	}
	if len(values) == 1 {
		return "target", values[0]
	}
	return "targets", strings.Join(values, ",")
}

type recoveryPlanJSONOutput struct {
	SchemaVersion          int                               `json:"schema_version"`
	Command                string                            `json:"command"`
	Mode                   string                            `json:"mode"`
	Phase                  string                            `json:"phase"`
	AuthorityKind          journal.RecoveryAuthorityKind     `json:"authority_kind,omitempty"`
	OperationID            string                            `json:"operation_id"`
	OperationDir           string                            `json:"operation_dir,omitempty"`
	Classification         string                            `json:"classification,omitempty"`
	ActionCount            int                               `json:"action_count"`
	CleanupObligationCount int                               `json:"cleanup_obligation_count"`
	HasErrors              bool                              `json:"has_errors"`
	Actions                []recoveryPlanJSONAction          `json:"actions"`
	CleanupObligations     []recoveryCleanupObligationOutput `json:"cleanup_obligations"`
	Errors                 []string                          `json:"errors,omitempty"`
}

type recoveryCleanupObligationOutput struct {
	Action      string `json:"action,omitempty"`
	Readiness   string `json:"readiness"`
	Reason      string `json:"reason,omitempty"`
	Scope       string `json:"scope"`
	Destination string `json:"destination"`
	Detail      string `json:"detail,omitempty"`
}

type recoveryPlanJSONAction struct {
	Kind        string                   `json:"kind"`
	Reason      string                   `json:"reason,omitempty"`
	Subject     *planJSONSubject         `json:"subject,omitempty"`
	ResourceID  string                   `json:"resource_id,omitempty"`
	Resource    *recoverPlanJSONResource `json:"resource,omitempty"`
	Target      string                   `json:"target,omitempty"`
	Targets     []string                 `json:"targets,omitempty"`
	Scope       string                   `json:"scope,omitempty"`
	Destination string                   `json:"destination,omitempty"`
	ContentPath string                   `json:"content_path,omitempty"`
	ContentKind string                   `json:"content_kind,omitempty"`
	BackupPath  string                   `json:"backup_path,omitempty"`
	BackupHash  string                   `json:"backup_hash,omitempty"`
	BackupKind  string                   `json:"backup_kind,omitempty"`
	Detail      string                   `json:"detail,omitempty"`
}

type recoverPlanJSONResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func PrintRecoverResultJSON(
	output io.Writer,
	mode string,
	disclosure journal.RecoverablePlan,
	execution *recoverworkflow.ExecutionResult,
	resultErr error,
) error {
	payload, err := recoveryJSONPayload(mode, disclosure, execution)
	if err != nil {
		return err
	}
	if resultErr != nil {
		payload.HasErrors = true
		payload.Errors = []string{RecoverResultError(disclosure, execution, resultErr).Error()}
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

// RecoverResultError applies the recovery authority's semantic error
// projection. Retained active recovery preserves actionable detail; terminal
// results and cleanup-only recovery expose path-neutral lifecycle facts.
func RecoverResultError(
	disclosure journal.RecoverablePlan,
	execution *recoverworkflow.ExecutionResult,
	resultErr error,
) error {
	if resultErr == nil {
		return nil
	}
	if execution != nil {
		return execution.SemanticError(resultErr)
	}
	if cleanup, ok := journal.JournalCleanupPlan(disclosure); ok {
		return journal.WrapCleanupFailure(cleanup.Action(), resultErr)
	}
	return resultErr
}

func recoveryJSONPayload(
	mode string,
	disclosure journal.RecoverablePlan,
	execution *recoverworkflow.ExecutionResult,
) (recoveryPlanJSONOutput, error) {
	phase := "planned"
	if execution != nil {
		if execution.OperationID() == "" ||
			execution.OperationID() != recoveryOperationID(disclosure) {
			return recoveryPlanJSONOutput{}, fmt.Errorf(
				"recovery execution result does not match the disclosed authority",
			)
		}
		phase = string(execution.Phase())
		current, retained := execution.CurrentDisclosure()
		if !retained {
			return recoveryPlanJSONOutput{
				SchemaVersion:      contractversion.RecoveryJSON,
				Command:            "recover",
				Mode:               mode,
				Phase:              phase,
				OperationID:        execution.OperationID(),
				Actions:            []recoveryPlanJSONAction{},
				CleanupObligations: []recoveryCleanupObligationOutput{},
			}, nil
		}
		if recoveryOperationID(current) != execution.OperationID() ||
			current.AuthorityKind() != execution.AuthorityKind() {
			return recoveryPlanJSONOutput{}, fmt.Errorf(
				"recovery execution result has inconsistent current authority",
			)
		}
		disclosure = current
	}
	if plan, ok := journal.ActiveRecoveryPlan(disclosure); ok {
		actions := plan.Actions()
		cleanupObligations := recoveryCleanupObligations(plan.RemovalCleanupObligations())
		return recoveryPlanJSONOutput{
			SchemaVersion:          contractversion.RecoveryJSON,
			Command:                "recover",
			Mode:                   mode,
			Phase:                  phase,
			AuthorityKind:          disclosure.AuthorityKind(),
			OperationID:            plan.OperationID(),
			OperationDir:           plan.OperationDir(),
			Classification:         string(plan.Classification()),
			ActionCount:            len(actions),
			CleanupObligationCount: len(cleanupObligations),
			HasErrors:              plan.HasErrors(),
			Actions:                recoveryPlanJSONActions(actions),
			CleanupObligations:     cleanupObligations,
		}, nil
	}
	if plan, ok := journal.JournalCleanupPlan(disclosure); ok {
		return recoveryPlanJSONOutput{
			SchemaVersion:      contractversion.RecoveryJSON,
			Command:            "recover",
			Mode:               mode,
			Phase:              phase,
			AuthorityKind:      disclosure.AuthorityKind(),
			OperationID:        plan.Authority().OperationID(),
			Classification:     string(plan.Classification()),
			ActionCount:        1,
			CleanupObligations: []recoveryCleanupObligationOutput{},
			Actions: []recoveryPlanJSONAction{{
				Kind: string(plan.Action()),
			}},
		}, nil
	}
	return recoveryPlanJSONOutput{}, fmt.Errorf("recovery disclosure is uninitialized")
}

func recoveryOperationID(disclosure journal.RecoverablePlan) string {
	if plan, ok := journal.ActiveRecoveryPlan(disclosure); ok {
		return plan.OperationID()
	}
	if plan, ok := journal.JournalCleanupPlan(disclosure); ok {
		return plan.Authority().OperationID()
	}
	return ""
}

func recoveryCleanupObligations(
	obligations []recovery.RemovalCleanupObligation,
) []recoveryCleanupObligationOutput {
	result := make([]recoveryCleanupObligationOutput, 0, len(obligations))
	for _, obligation := range obligations {
		result = append(result, recoveryCleanupObligationOutput{
			Action:      string(obligation.Action()),
			Readiness:   string(obligation.Readiness()),
			Reason:      string(obligation.Reason()),
			Scope:       string(obligation.Scope()),
			Destination: obligation.Destination().String(),
			Detail:      obligation.Detail(),
		})
	}
	return result
}

func recoveryPlanJSONActions(actions []recovery.Action) []recoveryPlanJSONAction {
	result := make([]recoveryPlanJSONAction, 0, len(actions))
	for _, action := range actions {
		subject, hasSubject := action.SubjectID()
		var entityID entity.ID
		if hasSubject {
			if projected, ok := topologyprojection.EntityID(subject); ok {
				entityID = projected
			}
		}
		consumerTargets := make([]string, 0, len(action.ConsumerTargets))
		for _, value := range action.ConsumerTargets {
			consumerTargets = append(consumerTargets, string(value))
		}
		var resource *recoverPlanJSONResource
		if entityID != (entity.ID{}) {
			resource = &recoverPlanJSONResource{
				Kind: string(entityID.Kind()),
				Name: entityID.Name(),
			}
		}
		result = append(result, recoveryPlanJSONAction{
			Kind:        string(action.Kind),
			Reason:      action.Reason,
			Subject:     planJSONSubjectFor(subject),
			ResourceID:  recoveryEntityIDString(entityID),
			Resource:    resource,
			Target:      string(action.Target),
			Targets:     consumerTargets,
			Scope:       string(action.Scope),
			Destination: action.Destination,
			ContentPath: action.ContentPath,
			ContentKind: string(action.ContentKind),
			BackupPath:  action.BackupPath,
			BackupHash:  action.BackupHash,
			BackupKind:  action.BackupKind,
			Detail:      action.Detail,
		})
	}

	return result
}

func recoveryEntityIDString(entityID entity.ID) string {
	if entityID == (entity.ID{}) {
		return ""
	}
	return string(entityID.Kind()) + "/" + entityID.Name()
}
