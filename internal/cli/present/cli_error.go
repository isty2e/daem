package clipresent

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/contractversion"
)

// CLIErrorKind is the command-owned failure classification, not a workflow stage.
type CLIErrorKind string

const (
	CLIErrorUsage     CLIErrorKind = "usage"
	CLIErrorOperation CLIErrorKind = "operation"
)

type cliErrorJSONOutput struct {
	SchemaVersion int                    `json:"schema_version"`
	ResultType    string                 `json:"result_type"`
	Command       string                 `json:"command"`
	Error         cliErrorJSONDetail     `json:"error"`
	Guidance      []cliErrorJSONGuidance `json:"guidance"`
}

type cliErrorJSONDetail struct {
	Kind    CLIErrorKind `json:"kind"`
	Message string       `json:"message"`
}

type cliErrorJSONGuidance struct {
	Kind    GuidanceKind `json:"kind"`
	Message string       `json:"message,omitempty"`
	Argv    []string     `json:"argv,omitempty"`
}

// PrintCLIErrorJSON projects an owned diagnostic, never an arbitrary error or
// captured output. The invocation owner decides whether this result is available.
func PrintCLIErrorJSON(output io.Writer, command string, kind CLIErrorKind, diagnostic FailureDiagnostic) error {
	if command == "" || diagnostic.message == "" || (kind != CLIErrorUsage && kind != CLIErrorOperation) {
		return fmt.Errorf("invalid common CLI failure")
	}

	guidance := make([]cliErrorJSONGuidance, 0, len(diagnostic.guidance))
	for _, item := range diagnostic.guidance {
		guidance = append(guidance, cliErrorJSONGuidance{
			Kind:    item.kind,
			Message: item.message,
			Argv:    item.argv,
		})
	}
	payload := cliErrorJSONOutput{
		SchemaVersion: contractversion.CLIErrorJSON,
		ResultType:    "cli_error",
		Command:       command,
		Error:         cliErrorJSONDetail{Kind: kind, Message: diagnostic.message},
		Guidance:      guidance,
	}

	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}
