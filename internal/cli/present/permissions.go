package clipresent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/subprocess/executioncontext"
	permissions "github.com/isty2e/daem/internal/workflow/protection"
)

type permissionRecoverOutput struct {
	SchemaVersion int                       `json:"schema_version"`
	Command       string                    `json:"command"`
	Mode          string                    `json:"mode"`
	AuthorityKind string                    `json:"authority_kind"`
	Phase         string                    `json:"phase"`
	HasErrors     bool                      `json:"has_errors"`
	Protections   []permissionRecoverRecord `json:"protections"`
	Errors        []string                  `json:"errors,omitempty"`
}

type permissionRecoverRecord struct {
	Invocation string                    `json:"invocation"`
	Phase      string                    `json:"phase"`
	Settlement string                    `json:"settlement"`
	Targets    []permissionRecoverTarget `json:"targets"`
	Reason     string                    `json:"reason,omitempty"`
}

type permissionRecoverTarget struct {
	Destination    string `json:"destination"`
	State          string `json:"state"`
	AccessExpanded bool   `json:"observed_access_expansion"`
	Reason         string `json:"reason,omitempty"`
}

func permissionReason(cause error) string {
	if cause == nil {
		return ""
	}
	if errors.Is(cause, context.Canceled) {
		return "canceled"
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	var protection *ownership.ProtectionRefusal
	if errors.As(cause, &protection) {
		return string(protection.Kind())
	}
	var observation *executioncontext.ExecutionObservationError
	if errors.As(cause, &observation) {
		return string(observation.Kind())
	}
	var permission *permissions.Failure
	if errors.As(cause, &permission) {
		return string(permission.Kind())
	}
	var root *rootedpath.Failure
	if errors.As(cause, &root) {
		return string(root.Kind())
	}
	if errors.Is(cause, os.ErrPermission) {
		return "permission_access_denied"
	}
	return "permission_safety_unresolved"
}

func permissionRecords(records []permissions.Record) []permissionRecoverRecord {
	result := make([]permissionRecoverRecord, 0, len(records))
	for _, record := range records {
		row := permissionRecoverRecord{Invocation: record.Invocation().String(), Phase: string(record.Phase()), Settlement: string(record.Settlement()), Reason: permissionReason(record.Cause())}
		for _, target := range record.Targets() {
			row.Targets = append(row.Targets, permissionRecoverTarget{Destination: target.Destination().String(), State: string(target.State()), AccessExpanded: target.Expanded(), Reason: permissionReason(target.Cause())})
		}
		result = append(result, row)
	}
	return result
}

func PrintPermissionRecoverPlan(output io.Writer, plan permissions.Plan) {
	fmt.Fprintln(output, "recover: permission safety only; no command replay or configuration-value restoration")
	printPermissionRecords(output, permissionRecords(plan.Records()))
	if plan.Blocker() != nil {
		fmt.Fprintln(output, "blocked: journal or file-set authority is not clear")
	}
}

func PrintPermissionRecoverResult(output io.Writer, result permissions.Result, cause error) {
	printPermissionRecords(output, permissionRecords(result.Records()))
	if result.Complete() && cause == nil {
		fmt.Fprintln(output, "permission safety recovery completed; installation and configuration values were not repaired")
	} else if result.Complete() {
		fmt.Fprintln(output, "permission records cleared; final operation validation failed; inspect current authority again")
	} else {
		fmt.Fprintln(output, "permission safety recovery did not complete; preserve pending records and inspect again")
	}
}

func printPermissionRecords(output io.Writer, records []permissionRecoverRecord) {
	for _, record := range records {
		fmt.Fprintf(output, "protection %s phase=%s settlement=%s", record.Invocation, record.Phase, record.Settlement)
		if record.Reason != "" {
			fmt.Fprintf(output, " reason=%s", record.Reason)
		}
		fmt.Fprintln(output)
		for _, target := range record.Targets {
			fmt.Fprintf(output, "  %q %s", Escape(target.Destination), target.State)
			if target.Reason != "" {
				fmt.Fprintf(output, " reason=%s", target.Reason)
			}
			if target.AccessExpanded {
				fmt.Fprint(output, "; access had expanded; temporary access may have occurred")
			}
			fmt.Fprintln(output)
		}
	}
}

func PrintPermissionRecoverJSON(output io.Writer, mode string, plan permissions.Plan, result *permissions.Result, cause error) error {
	value := permissionRecoverOutput{SchemaVersion: contractversion.RecoveryJSON, Command: "recover", Mode: mode, AuthorityKind: "permission_safety", Phase: "planned", HasErrors: plan.HasErrors(), Protections: permissionRecords(plan.Records())}
	if plan.Blocker() != nil {
		value.Errors = append(value.Errors, "journal_or_file_set_authority_unresolved")
	}
	if result != nil {
		value.Protections = permissionRecords(result.Records())
		value.HasErrors = cause != nil || !result.Complete()
		value.Phase = string(result.Phase())
	}
	if cause != nil {
		value.Errors = append(value.Errors, permissionReason(cause))
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
