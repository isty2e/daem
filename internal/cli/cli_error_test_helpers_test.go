package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/isty2e/daem/test/testkit/clijson"
)

func refusalDiagnosticText(t *testing.T, command string, stdout, stderr *bytes.Buffer, commonJSON bool) string {
	t.Helper()
	return clijson.FailureText(t, stdout.String(), stderr.String(), command, "operation", commonJSON)
}

func commonFailureDiagnosticText(t *testing.T, command, expectedKind string, stdout, stderr *bytes.Buffer) string {
	t.Helper()
	return clijson.FailureText(t, stdout.String(), stderr.String(), command, expectedKind, true)
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
