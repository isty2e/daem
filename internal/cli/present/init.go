package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	initworkflow "github.com/isty2e/daem/internal/workflow/init"
)

const initJSONSchemaVersion = 1

func PrintInitPlanWithOptions(output io.Writer, label string, plan initworkflow.Plan, options HumanOptions) {
	switch label {
	case "init":
		fmt.Fprintf(output, "init: %s manifest\n", plan.Action)
	default:
		fmt.Fprintf(output, "%s: manifest\n", label)
	}
	fmt.Fprintf(output, "manifest: %s\n", Escape(plan.ManifestPath))
	if options.Verbose {
		fmt.Fprintln(output, "planned:")
		fmt.Fprint(output, string(plan.Content))
	}
	if label == "init" {
		fmt.Fprintln(output, "next: rerun daem init without --dry-run")
	} else {
		PrintShellCommand(output, "next: run ", "daem", "lock", "--manifest", plan.ManifestPath, "--dry-run")
	}
}

type initJSONOutput struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	Mode          string `json:"mode"`
	Action        string `json:"action"`
	ManifestPath  string `json:"manifest_path"`
	Content       string `json:"content"`
}

func PrintInitJSON(output io.Writer, mode string, plan initworkflow.Plan) error {
	payload := initJSONOutput{
		SchemaVersion: initJSONSchemaVersion,
		Command:       "init",
		Mode:          mode,
		Action:        plan.Action,
		ManifestPath:  plan.ManifestPath,
		Content:       string(plan.Content),
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
