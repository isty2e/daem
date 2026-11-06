package status

import (
	"errors"
	"testing"

	ownershipobserve "github.com/isty2e/daem/internal/assurance/observe/ownership"
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
			clear, err := ownershipobserve.KnownProtections(nil)
			if err != nil {
				t.Fatal(err)
			}
			test.result.PermissionProtection = clear
			if got := test.result.CheckClean(); got != test.want {
				t.Fatalf("CheckClean=%t, want %t", got, test.want)
			}
		})
	}
}

func TestCheckCleanRequiresKnownOwnerProtectionInventory(t *testing.T) {
	for _, observation := range []ownershipobserve.ProtectionObservation{
		{}, ownershipobserve.UnavailableProtections(errors.New("inventory unavailable")), ownershipobserve.UnsupportedProtections(),
	} {
		if (CommandResult{PermissionProtection: observation}).CheckClean() {
			t.Fatalf("uncertified owner was clean: %s", observation.State())
		}
	}
}
