package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

const recoveryJSONSchemaVersion = 3

func PrintRecoverPlanWithOptions(output io.Writer, plan recovery.Plan, options HumanOptions) {
	fmt.Fprintf(output, "recover: %s\n", plan.Classification())
	if len(plan.Actions()) == 0 {
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
	SchemaVersion  int                      `json:"schema_version"`
	Command        string                   `json:"command"`
	Mode           string                   `json:"mode"`
	OperationID    string                   `json:"operation_id"`
	OperationDir   string                   `json:"operation_dir"`
	Classification string                   `json:"classification"`
	ActionCount    int                      `json:"action_count"`
	HasErrors      bool                     `json:"has_errors"`
	Actions        []recoveryPlanJSONAction `json:"actions"`
	Errors         []string                 `json:"errors,omitempty"`
}

type recoveryPlanJSONAction struct {
	Kind        string                  `json:"kind"`
	Reason      string                  `json:"reason"`
	Subject     *planJSONSubject        `json:"subject,omitempty"`
	ResourceID  string                  `json:"resource_id"`
	Resource    recoverPlanJSONResource `json:"resource"`
	Target      string                  `json:"target,omitempty"`
	Targets     []string                `json:"targets,omitempty"`
	Scope       string                  `json:"scope"`
	Destination string                  `json:"destination"`
	ContentPath string                  `json:"content_path,omitempty"`
	ContentKind string                  `json:"content_kind,omitempty"`
	BackupPath  string                  `json:"backup_path"`
	BackupHash  string                  `json:"backup_hash"`
	BackupKind  string                  `json:"backup_kind"`
	Detail      string                  `json:"detail"`
}

type recoverPlanJSONResource struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func PrintRecoverResultJSON(output io.Writer, mode string, plan recovery.Plan, resultErr error) error {
	actions := plan.Actions()
	payload := recoveryPlanJSONOutput{
		SchemaVersion:  recoveryJSONSchemaVersion,
		Command:        "recover",
		Mode:           mode,
		OperationID:    plan.OperationID(),
		OperationDir:   plan.OperationDir(),
		Classification: string(plan.Classification()),
		ActionCount:    len(actions),
		HasErrors:      plan.HasErrors(),
		Actions:        recoveryPlanJSONActions(actions),
	}
	if resultErr != nil {
		payload.HasErrors = true
		payload.Errors = []string{resultErr.Error()}
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
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
		result = append(result, recoveryPlanJSONAction{
			Kind:        string(action.Kind),
			Reason:      action.Reason,
			Subject:     planJSONSubjectFor(subject),
			ResourceID:  recoveryEntityIDString(entityID),
			Resource:    recoverPlanJSONResource{Kind: string(entityID.Kind()), Name: entityID.Name()},
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
