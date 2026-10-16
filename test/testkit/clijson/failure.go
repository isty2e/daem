package clijson

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

type Failure struct {
	SchemaVersion int                            `json:"schema_version"`
	ResultType    string                         `json:"result_type"`
	Command       string                         `json:"command"`
	Error         struct{ Kind, Message string } `json:"error"`
	Guidance      []FailureGuidance              `json:"guidance"`
}

type FailureGuidance struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message,omitempty"`
	Argv    []string `json:"argv,omitempty"`
}

func DecodeFailure(t testing.TB, content string, command, kind string) Failure {
	t.Helper()
	if !utf8.ValidString(content) {
		t.Fatal("failure JSON is not valid UTF-8")
	}
	decoder := json.NewDecoder(strings.NewReader(content))
	decoder.DisallowUnknownFields()
	var result Failure
	if err := decoder.Decode(&result); err != nil {
		t.Fatalf("decode common failure: %v; stdout=%q", err, content)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("common failure contains trailing output: %v", err)
	}
	if result.SchemaVersion != 1 || result.ResultType != "cli_error" || result.Command != command || result.Error.Kind != kind || result.Error.Message == "" || result.Guidance == nil {
		t.Fatalf("incorrect common-failure identity: %+v", result)
	}
	for _, guidance := range result.Guidance {
		switch guidance.Kind {
		case "note", "instruction":
			if guidance.Message == "" || guidance.Argv != nil {
				t.Fatalf("incorrect text guidance: %+v", guidance)
			}
		case "command":
			if len(guidance.Argv) == 0 || guidance.Argv[0] != "daem" {
				t.Fatalf("incorrect command guidance: %+v", guidance)
			}
		default:
			t.Fatalf("unknown guidance kind: %+v", guidance)
		}
	}
	return result
}

// FailureText checks the expected transport before exposing decoded diagnostic
// strings to content assertions. It never chooses a format from observed bytes.
func FailureText(t testing.TB, stdout, stderr, command, kind string, commonJSON bool) string {
	t.Helper()
	if !commonJSON {
		if stdout != "" {
			t.Fatalf("human failure emitted stdout: %q", stdout)
		}
		return stderr
	}
	if stderr != "" {
		t.Fatalf("common JSON failure emitted stderr: %q", stderr)
	}
	result := DecodeFailure(t, stdout, command, kind)
	text := []string{result.Error.Message}
	for _, guidance := range result.Guidance {
		text = append(text, guidance.Message)
		if len(guidance.Argv) != 0 {
			text = append(text, strings.Join(guidance.Argv, " "))
		}
	}
	return strings.Join(text, "\n")
}

// CommandName is an expected leaf identity for test calls, not an argv parser.
func CommandName(args []string) string {
	switch args[0] {
	case "add", "remove", "unmanage", "list", "probe", "refresh", "migrate":
		return strings.Join(args[:2], " ")
	default:
		return args[0]
	}
}
