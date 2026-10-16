package cli

import (
	"bytes"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestAuthoringRequestsKeepCanonicalRoutesAndFirstShapeFailure(t *testing.T) {
	for _, test := range []struct{ command, operands []string }{
		{[]string{"add", "instruction"}, []string{"guide", "source.md"}},
		{[]string{"add", "hook"}, []string{"hook", "PreToolUse", "command"}},
		{[]string{"add", "mcp-server"}, []string{"server", "command"}},
		{[]string{"add", "extension"}, []string{"extension", "source"}},
		{[]string{"add", "skill"}, []string{"source"}},
		{[]string{"add", "skill-group"}, []string{"source", "--member", "review"}},
		{[]string{"remove", "instruction"}, []string{"guide"}},
		{[]string{"remove", "hook"}, []string{"hook"}},
		{[]string{"remove", "mcp-server"}, []string{"server"}},
		{[]string{"remove", "extension"}, []string{"extension"}},
		{[]string{"remove", "skill"}, []string{"skill"}},
		{[]string{"unmanage", "extension"}, []string{"extension"}},
	} {
		t.Run(strings.Join(test.command, " "), func(t *testing.T) {
			root, _ := manifestHintWorkspace(t)
			before := snapshotApplyInputFiles(t, root)
			base := append(slices.Clone(test.command), test.operands...)
			for _, invalid := range [][]string{
				{"--unknown", "extra", "--json"},
				{"--json", "--json=maybe"},
			} {
				var stdout, stderr bytes.Buffer
				code := runWithoutTerminal(append(slices.Clone(base), invalid...), &stdout, &stderr)
				diagnostic := commonFailureDiagnosticText(t, strings.Join(test.command, " "), "usage", &stdout, &stderr)
				if code != 2 {
					t.Fatalf("code=%d, want usage refusal", code)
				}
				if invalid[0] == "--unknown" && (!strings.Contains(diagnostic, "flag provided but not defined: -unknown") || strings.Contains(diagnostic, `unexpected argument "extra"`)) {
					t.Fatalf("later shape failure overwrote first cause: %q", diagnostic)
				}
			}
			var stdout, stderr bytes.Buffer
			code := runWithoutTerminal(append(slices.Clone(base), "--unknown", "--json=false"), &stdout, &stderr)
			if code != 2 || stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("explicit human mode changed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if after := snapshotApplyInputFiles(t, root); !reflect.DeepEqual(before, after) {
				t.Fatal("malformed request changed workspace files")
			}
		})
	}
}

func TestAuthoringRequestsKeepDeclaredFormatLookingValuesOpaque(t *testing.T) {
	for _, args := range [][]string{
		{"add", "mcp-server", "server", "command", "--arg", "--json", "--unknown"},
		{"add", "hook", "hook", "PreToolUse", "command", "--matcher", "--json", "--unknown"},
		{"add", "skill", "source", "--name", "--json", "--unknown"},
		{"add", "skill", "--", "--json"},
	} {
		manifestHintWorkspace(t)
		var stdout, stderr bytes.Buffer
		code := runWithoutTerminal(args, &stdout, &stderr)
		if code == 0 || stdout.Len() != 0 || stderr.Len() == 0 {
			t.Fatalf("data selected output mode: args=%q code=%d stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
		}
	}
}
