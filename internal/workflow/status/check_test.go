package status

import (
	"testing"

	"github.com/isty2e/daem/internal/findings"
)

func TestCheckCleanSeparatesStateFromDiagnosticGrade(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result CommandResult
		want   bool
	}{
		{name: "clean observed state", want: true},
		{name: "missing lockfile", result: CommandResult{LockfileMissing: true}},
		{
			name: "warning-only diagnostics",
			result: CommandResult{Diagnostics: []findings.Diagnostic{
				{Severity: findings.SeverityWarn, Code: "hook.command.portability"},
			}},
			want: true,
		},
		{
			name: "error diagnostics without pending state",
			result: CommandResult{Diagnostics: []findings.Diagnostic{
				{Severity: findings.SeverityError, Code: "skill.compat.manual"},
			}},
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.result.CheckClean(); got != test.want {
				t.Fatalf("CheckClean=%t, want %t", got, test.want)
			}
		})
	}
}
