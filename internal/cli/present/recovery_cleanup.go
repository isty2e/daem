package clipresent

import (
	"fmt"
	"io"

	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

type recoveryCleanupJSON struct {
	Kind    string `json:"kind"`
	Outcome string `json:"outcome,omitempty"`
}

func recoveryCleanupJSONRows(steps []applyworkflow.RecoveryCleanupStep, write bool) []recoveryCleanupJSON {
	if len(steps) == 0 {
		return nil
	}
	rows := make([]recoveryCleanupJSON, 0, len(steps))
	for _, step := range steps {
		row := recoveryCleanupJSON{Kind: string(step.Kind)}
		if write {
			row.Outcome = string(step.Outcome)
			if row.Outcome == "" {
				row.Outcome = "not_attempted"
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// PrintRecoveryCleanup separates cleanup from normal reconciliation actions.
func PrintRecoveryCleanup(w io.Writer, steps []applyworkflow.RecoveryCleanupStep) {
	for _, step := range steps {
		var subject string
		switch step.Kind {
		case "journal_retirement":
			subject = "already-retired apply journal"
		case "declaration_pair":
			subject = "completed manifest and lockfile update"
		}
		var outcome string
		switch step.Outcome {
		case "":
			outcome = "clear the remaining recovery files"
		case "retired":
			outcome = "recovery cleared"
		case "not_attempted":
			outcome = "cleanup not attempted"
		case "still_fenced":
			outcome = "recovery still required"
		case "unconfirmed":
			outcome = "cleanup could not be confirmed; preserve remaining evidence"
		}
		fmt.Fprintf(w, "recovery cleanup: %s: %s\n", subject, outcome)
	}
}
