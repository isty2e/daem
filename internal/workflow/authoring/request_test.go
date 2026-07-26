package authoring

import (
	"path"
	"strings"
	"testing"
)

func TestSafeSegmentRequestNamesPreserveFamilyErrorLabels(t *testing.T) {
	tests := []struct {
		name  string
		clean func(string) (string, error)
		label string
	}{
		{name: "hook", clean: CleanHookName, label: "hook name"},
		{name: "instruction", clean: CleanInstructionName, label: "instruction name"},
		{name: "skill group", clean: CleanSkillGroupName, label: "skill-group member"},
	}
	invalid := []string{"", ".", "..", "~name", "a/b", `a\b`}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, value := range invalid {
				_, err := test.clean(value)
				if err == nil || !strings.Contains(err.Error(), test.label+" must be a safe single path segment") {
					t.Fatalf("clean(%q) error = %v", value, err)
				}
			}
			got, err := test.clean("  한글-name  ")
			if err != nil || got != "한글-name" {
				t.Fatalf("clean valid Unicode = %q, %v", got, err)
			}
		})
	}
}

func TestConsolidatedSafeSegmentCleanerMatchesEachFormerContract(t *testing.T) {
	alphabet := []string{"", "a", ".", "~", "/", `\`, " ", ":", "한"}
	values := append([]string(nil), alphabet...)
	for _, left := range alphabet {
		for _, right := range alphabet {
			values = append(values, left+right)
		}
	}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		formerHookAccepted := trimmed != "" && trimmed != "." && trimmed != ".." &&
			!strings.HasPrefix(trimmed, "~") && !strings.Contains(trimmed, "/") && !strings.Contains(trimmed, `\`)
		formerInstructionAccepted := formerHookAccepted && path.Clean(trimmed) == trimmed
		formerGroupAccepted := formerInstructionAccepted

		for _, test := range []struct {
			name string
			want bool
			call func(string) (string, error)
		}{
			{name: "hook", want: formerHookAccepted, call: CleanHookName},
			{name: "instruction", want: formerInstructionAccepted, call: CleanInstructionName},
			{name: "skill group", want: formerGroupAccepted, call: CleanSkillGroupName},
		} {
			_, err := test.call(value)
			if (err == nil) != test.want {
				t.Fatalf("%s clean(%q) success = %t, former contract = %t", test.name, value, err == nil, test.want)
			}
		}
	}
}

func TestStableTokenRequestNamesRemainFamilySpecific(t *testing.T) {
	for _, test := range []struct {
		name  string
		clean func(string) (string, error)
	}{
		{name: "extension", clean: CleanExtensionID},
		{name: "mcp", clean: CleanMCPServerName},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.clean("  server.v1_name  ")
			if err != nil || got != "server.v1_name" {
				t.Fatalf("clean valid token = %q, %v", got, err)
			}
			for _, value := range []string{"-starts-with-punctuation", "unicode-한글", "has/slash"} {
				if _, err := test.clean(value); err == nil {
					t.Fatalf("clean(%q) unexpectedly succeeded", value)
				}
			}
		})
	}
}
