package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunAddSkillSpacedTargetListPrintsTargetHint(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	scenarios := []struct {
		name string
		args []string
		arg  string
	}{
		{
			name: "single target value then supported target argument",
			args: []string{"add", "skill", "skills/oracle", "--target", "codex", "claude-code", "--dry-run"},
			arg:  "claude-code",
		},
		{
			name: "comma target value then supported target argument",
			args: []string{"add", "skill", "skills/oracle", "--target", "codex,claude-code", "pi", "--dry-run"},
			arg:  "pi",
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			exitCode := runWithoutTerminal(scenario.args, &stdout, &stderr)
			if exitCode != 2 {
				t.Fatalf("exitCode = %d, want 2", exitCode)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
			for _, want := range []string{
				`add failed: unexpected argument "` + scenario.arg + `"`,
				"next: repeat the flag: --target codex --target claude-code",
			} {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want %q", stderr.String(), want)
				}
			}
		})
	}
}

func TestRunStatusSpacedTargetListPrintsTargetHint(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"status", "--target", "codex", "claude-code"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{
		`unexpected argument "claude-code"`,
		"next: repeat the flag: --target codex --target claude-code",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want %q", stderr.String(), want)
		}
	}
}

func TestRunStatusUnexpectedNonTargetArgumentDoesNotPrintTargetHint(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	exitCode := runWithoutTerminal([]string{"status", "--target", "codex", "extra"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exitCode = %d, want 2", exitCode)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), `unexpected argument "extra"`) {
		t.Fatalf("stderr = %q, want unexpected argument diagnostic", stderr.String())
	}
	if strings.Contains(stderr.String(), "pass multiple targets") {
		t.Fatalf("stderr = %q, want no target-list hint", stderr.String())
	}
}
