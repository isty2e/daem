package cli

import (
	"io"
	"slices"
	"strings"
	"testing"
)

func TestAuthoringPlanPreservesOperandAndFlagBoundaries(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		positionalCount int
		wantPositionals []string
		wantFlagArgs    []string
		wantError       string
	}{
		{
			name:            "interspersed valued and boolean flags",
			args:            []string{"--target", "codex", "name", "--dry-run", "source"},
			positionalCount: 2,
			wantPositionals: []string{"name", "source"},
			wantFlagArgs:    []string{"--target", "codex", "--dry-run"},
		},
		{
			name:            "inline valued flag",
			args:            []string{"name", "source", "--target=codex"},
			positionalCount: 2,
			wantPositionals: []string{"name", "source"},
			wantFlagArgs:    []string{"--target=codex"},
		},
		{
			name:            "separator admits help token as operand",
			args:            []string{"--dry-run", "--", "--help"},
			positionalCount: 1,
			wantPositionals: []string{"--help"},
			wantFlagArgs:    []string{"--dry-run"},
		},
		{
			name:            "separator makes later flag-shaped token positional",
			args:            []string{"name", "--", "--target"},
			positionalCount: 2,
			wantPositionals: []string{"name", "--target"},
		},
		{
			name:            "unknown flag after operands rejected",
			args:            []string{"name", "source", "--unknown", "value", "tail"},
			positionalCount: 2,
			wantError:       "flag provided but not defined: -unknown",
		},
		{
			name:            "unknown flag before operands rejected",
			args:            []string{"--unknown", "name", "source"},
			positionalCount: 2,
			wantError:       "flag provided but not defined: -unknown",
		},
		{
			name:            "single hyphen option rejected",
			args:            []string{"name", "source", "-target", "codex"},
			positionalCount: 2,
			wantError:       "flag provided but not defined: -target",
		},
		{
			name:            "single hyphen operand admitted after separator",
			args:            []string{"--", "-source"},
			positionalCount: 1,
			wantPositionals: []string{"-source"},
		},
		{
			name:            "known flag missing value",
			args:            []string{"name", "source", "--target"},
			positionalCount: 2,
			wantError:       "flag needs an argument: -target",
		},
		{
			name:            "extra positional rejected",
			args:            []string{"name", "source", "extra"},
			positionalCount: 2,
			wantError:       `unexpected argument "extra"`,
		},
		{
			name:            "repeated values keep order",
			args:            []string{"source", "--member", "review", "--member=test", "--target", "codex"},
			positionalCount: 1,
			wantPositionals: []string{"source"},
			wantFlagArgs:    []string{"--member", "review", "--member=test", "--target", "codex"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			flags := newCommandFlagSet([]string{"fixture"}, io.Discard)
			flags.String("target", "", "")
			flags.String("member", "", "")
			for _, name := range []string{"dry-run", "diff", "json", "verbose"} {
				flags.Bool(name, false, "")
			}
			plan := prepareAuthoringArguments(flags, test.args, test.positionalCount)
			positionals, err := plan.operands()
			flagArgs := plan.options
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("argument plan returned error: %v", err)
			}
			if !slices.Equal(positionals, test.wantPositionals) {
				t.Fatalf("positionals = %#v, want %#v", positionals, test.wantPositionals)
			}
			if !slices.Equal(flagArgs, test.wantFlagArgs) {
				t.Fatalf("flagArgs = %#v, want %#v", flagArgs, test.wantFlagArgs)
			}
		})
	}
}

func TestCommandHelpRequestedHonorsSeparator(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "long help", args: []string{"name", "--help"}, want: true},
		{name: "short help before separator", args: []string{"-h", "--", "--help"}, want: true},
		{name: "literal help after separator", args: []string{"--", "--help"}, want: false},
		{name: "literal short help after separator", args: []string{"name", "--", "-h"}, want: false},
		{name: "help topic", args: []string{"help"}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := commandHelpRequested(test.args); got != test.want {
				t.Fatalf("commandHelpRequested(%#v) = %t, want %t", test.args, got, test.want)
			}
		})
	}
}
