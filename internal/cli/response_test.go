package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

func TestCommandResponseReservesOrdinaryResultBeforeProjection(t *testing.T) {
	for _, stdout := range []io.Writer{nil, io.Discard, &bytes.Buffer{}} {
		var stderr bytes.Buffer
		response := newCommandResponse("apply", requestedJSON, stdout, &stderr)
		cause := errors.New("projection failed before writing")
		if err := response.ordinaryResult(func() error { return cause }); !errors.Is(err, cause) {
			t.Fatal("ordinary projection failure was replaced")
		}
		if code := response.failure(operationFailure, clipresent.NewFailureDiagnostic("apply result projection failed")); code != 1 || stderr.Len() == 0 {
			t.Fatal("late failure lost its exit or stderr report")
		}
		if output, ok := stdout.(*bytes.Buffer); ok && output.Len() != 0 {
			t.Fatalf("empty delivery was repaired with a common result: %q", output.String())
		}
		called := false
		if err := response.ordinaryResult(func() error { called = true; return nil }); !errors.Is(err, errTerminalResponseSelected) || called {
			t.Fatal("a second ordinary result was projected")
		}
	}
}

func TestCommandResponseClassificationAndSingleTerminalDocument(t *testing.T) {
	for _, class := range []commandFailureClass{usageFailure, operationFailure} {
		var stdout, stderr bytes.Buffer
		response := newCommandResponse("probe mcp-server", requestedJSON, &stdout, &stderr)
		code := response.failure(class, clipresent.NewFailureDiagnostic("request rejected"))
		var payload struct {
			Command string `json:"command"`
			Error   struct {
				Kind clipresent.CLIErrorKind `json:"kind"`
			} `json:"error"`
		}
		decoder := json.NewDecoder(&stdout)
		if err := decoder.Decode(&payload); err != nil || payload.Command != "probe mcp-server" || payload.Error.Kind != class.jsonKind() || code != class.exitCode() {
			t.Fatalf("classification drift: %v, %#v, %d", err, payload, code)
		}
		response.failure(operationFailure, clipresent.NewFailureDiagnostic("later failure"))
		if err := decoder.Decode(&payload); !errors.Is(err, io.EOF) || stderr.Len() == 0 {
			t.Fatalf("a second failure document was published: %v", err)
		}
	}
}

type responseFailureWriter struct {
	calls int
}

func (writer *responseFailureWriter) Write([]byte) (int, error) {
	writer.calls++
	return 0, errors.New("private output transport detail")
}

func TestCommonFailureDeliveryDoesNotReplaceClassificationOrReportTwice(t *testing.T) {
	var stderr bytes.Buffer
	stdout := &responseFailureWriter{}
	code := RunWithOptions([]string{"version", "extra", "--json"}, RunOptions{Stdout: stdout, Stderr: &stderr})
	if code != 2 || stdout.calls != 1 || strings.Count(stderr.String(), "\n") != 1 || strings.Contains(stderr.String(), "private output transport detail") {
		t.Fatalf("failure was retried, reclassified or leaked transport evidence: code=%d calls=%d stderr=%q", code, stdout.calls, stderr.String())
	}
}

func TestRunVersionUsesRequestedFormatWithoutChangingItsValidGrammar(t *testing.T) {
	for _, test := range []struct {
		args   []string
		code   int
		common bool
		json   bool
	}{
		{[]string{"version", "extra", "--json"}, 2, true, true},
		{[]string{"version", "--json", "extra"}, 2, true, true},
		{[]string{"version", "--json", "--json"}, 2, true, true},
		{[]string{"version", "--json"}, 0, false, true},
		{[]string{"version", "--json=false"}, 2, false, false},
		{[]string{"version", "--", "--json"}, 2, false, false},
		{[]string{"--version", "--json"}, 2, false, false},
		{[]string{"--json", "version"}, 2, false, false},
	} {
		t.Run(flagCaseName(test.args), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := RunWithOptions(test.args, RunOptions{Stdout: &stdout, Stderr: &stderr})
			if code != test.code {
				t.Fatalf("code=%d, want %d; stderr=%q", code, test.code, stderr.String())
			}
			if !test.json {
				if stdout.Len() != 0 || stderr.Len() == 0 {
					t.Fatalf("unadmitted format gained output: %q, %q", stdout.String(), stderr.String())
				}
				return
			}
			var payload struct {
				ResultType string `json:"result_type"`
				Command    string `json:"command"`
				Version    string `json:"version"`
				Error      struct {
					Kind string `json:"kind"`
				} `json:"error"`
			}
			decoder := json.NewDecoder(&stdout)
			if err := decoder.Decode(&payload); err != nil || stderr.Len() != 0 {
				t.Fatalf("invalid or mixed JSON: %v, %q", err, stderr.String())
			}
			if test.common && (payload.ResultType != "cli_error" || payload.Command != "version" || payload.Error.Kind != "usage") {
				t.Fatalf("lost common failure identity: %#v", payload)
			}
			if !test.common && payload.Version == "" {
				t.Fatalf("ordinary version result was replaced: %#v", payload)
			}
			if err := decoder.Decode(&payload); !errors.Is(err, io.EOF) {
				t.Fatalf("more than one document: %v", err)
			}
		})
	}
}
