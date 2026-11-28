package clipresent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	recoverworkflow "github.com/isty2e/daem/internal/workflow/recover"
)

func PrintDeclarationRecoveryJSON(output io.Writer, mode string, plan recoverworkflow.DeclarationRecoveryPlan, phase recoverworkflow.DeclarationRecoveryPhase, err error) error {
	payload := struct {
		SchemaVersion   int                                      `json:"schema_version"`
		Command         string                                   `json:"command"`
		Mode            string                                   `json:"mode"`
		AuthorityKind   string                                   `json:"authority_kind"`
		Phase           recoverworkflow.DeclarationRecoveryPhase `json:"phase"`
		Action          fileset.RecoveryDisposition              `json:"action,omitempty"`
		RemovesLockfile bool                                     `json:"removes_lockfile,omitempty"`
		HasErrors       bool                                     `json:"has_errors"`
		Errors          []string                                 `json:"errors,omitempty"`
	}{
		SchemaVersion: contractversion.RecoveryJSON, Command: "recover", Mode: mode,
		AuthorityKind: "declaration_transaction", Phase: phase, HasErrors: err != nil,
	}
	if phase == recoverworkflow.DeclarationRecoveryPlanned || phase == recoverworkflow.DeclarationRecoveryCompleted {
		payload.Action = plan.Action
		payload.RemovesLockfile = plan.RemovesLockfile
	}
	if err != nil {
		payload.Errors = []string{declarationRecoveryFailure(phase, err)}
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func PrintDeclarationRecovery(output io.Writer, plan recoverworkflow.DeclarationRecoveryPlan, phase recoverworkflow.DeclarationRecoveryPhase, err error) error {
	if err != nil {
		_, writeErr := fmt.Fprintln(output, declarationRecoveryFailure(phase, err))
		return writeErr
	}
	var text strings.Builder
	if phase == recoverworkflow.DeclarationRecoveryPlanned {
		if plan.Action == fileset.RecoveryRollback {
			fmt.Fprintln(&text, "Would roll back the interrupted declaration transaction to its recorded before-state.")
			if plan.RemovesLockfile {
				fmt.Fprintln(&text, "This removes the lockfile created by the interrupted write.")
			}
		} else {
			fmt.Fprintln(&text, "Both files already match the completed write. Would clear only the recovery record.")
		}
		for _, file := range []struct{ label, path string }{{"manifest", plan.ManifestPath}, {"lockfile", plan.LockfilePath}} {
			if visibleRecoveryReference(file.path) {
				fmt.Fprintf(&text, "%s: %s\n", file.label, Quote(file.path))
			} else {
				fmt.Fprintf(&text, "%s: path withheld\n", file.label)
			}
		}
	} else if plan.Action == fileset.RecoveryRollback {
		fmt.Fprintln(&text, "Rolled back the interrupted declaration transaction.")
	} else {
		fmt.Fprintln(&text, "Cleared the recovery record without changing the manifest or lockfile.")
	}
	_, writeErr := io.WriteString(output, text.String())
	return writeErr
}

func declarationRecoveryFailure(phase recoverworkflow.DeclarationRecoveryPhase, err error) string {
	message := RecoverPlanningDiagnostic(err, HumanOptions{}).Message()
	switch phase {
	case recoverworkflow.DeclarationRecoveryCompleted:
		return "Declaration recovery completed, but releasing its leases failed. " + message
	case recoverworkflow.DeclarationRecoveryUnconfirmed:
		return "Declaration recovery could not be confirmed. Preserve the files and any remaining recovery record and inspect a new preview. " + message
	default:
		return message
	}
}
