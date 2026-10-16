package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

// refusalDiagnosticText checks the selected transport and returns every admitted
// diagnostic string so secrecy/budget assertions cover guidance as well as cause.
func refusalDiagnosticText(t *testing.T, command string, stdout, stderr *bytes.Buffer, commonJSON bool) string {
	t.Helper()
	if !commonJSON {
		if stdout.Len() != 0 {
			t.Fatalf("human refusal emitted stdout: %q", stdout.String())
		}
		return stderr.String()
	}
	return commonFailureDiagnosticText(t, command, "operation", stdout, stderr)
}

func commonFailureDiagnosticText(t *testing.T, command, expectedKind string, stdout, stderr *bytes.Buffer) string {
	t.Helper()
	if stderr.Len() != 0 {
		t.Fatalf("common JSON refusal leaked human stderr: %q", stderr.String())
	}
	var payload struct {
		SchemaVersion int    `json:"schema_version"`
		ResultType    string `json:"result_type"`
		Command       string `json:"command"`
		Error         struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
		Guidance []struct {
			Kind    string   `json:"kind"`
			Message string   `json:"message,omitempty"`
			Argv    []string `json:"argv,omitempty"`
		} `json:"guidance"`
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != 1 || payload.ResultType != "cli_error" || payload.Command != command ||
		payload.Error.Kind != expectedKind || payload.Error.Message == "" || payload.Guidance == nil {
		t.Fatalf("refusal lost its identity/cause: %#v", payload)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("refusal emitted multiple documents: %v", err)
	}
	texts := []string{payload.Error.Message}
	for _, guidance := range payload.Guidance {
		switch guidance.Kind {
		case "note", "instruction":
			if guidance.Message == "" || len(guidance.Argv) != 0 {
				t.Fatalf("invalid text guidance: %#v", guidance)
			}
		case "command":
			if len(guidance.Argv) == 0 || guidance.Argv[0] != "daem" {
				t.Fatalf("invalid command guidance: %#v", guidance)
			}
		default:
			t.Fatalf("unknown guidance kind: %q", guidance.Kind)
		}
		texts = append(texts, guidance.Message)
		texts = append(texts, guidance.Argv...)
	}
	return strings.Join(texts, "\n")
}

func hasFailureCommand(t *testing.T, stdout *bytes.Buffer, argv []string) bool {
	t.Helper()
	var payload struct {
		Guidance []struct {
			Kind string
			Argv []string
		}
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(payload.Guidance, func(item struct {
		Kind string
		Argv []string
	},
	) bool {
		return item.Kind == "command" && slices.Equal(item.Argv, argv)
	})
}
