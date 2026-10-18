package clipresent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	probeworkflow "github.com/isty2e/daem/internal/workflow/probe"
)

func TestMCPProbeSelectionDiagnosticRetainsFacts(t *testing.T) {
	const name = "instructions/mcp_server:%25"
	for _, test := range []struct {
		failure error
		want    string
	}{
		{
			failure: probeworkflow.NoAdmittedBindingError{Name: name, Target: target.TargetOpenCode, Scope: target.ScopeGlobal},
			want:    `mcp-server "instructions/mcp_server:%25" has no admitted runtime-probe row matching target=opencode scope=global`,
		},
		{
			failure: probeworkflow.NoAdmittedBindingError{Name: name},
			want:    `mcp-server "instructions/mcp_server:%25" has no admitted runtime-probe row matching target= scope=`,
		},
		{
			failure: probeworkflow.AmbiguousBindingError{Name: name, Matches: 3},
			want:    `mcp-server "instructions/mcp_server:%25" is ambiguous across 3 runtime-probe rows; narrow with --target and --scope`,
		},
		{
			failure: probeworkflow.BindingAdmissionError{Name: name, Err: errors.New("original admission cause")},
			want:    `mcp-server "instructions/mcp_server:%25" runtime-probe admission: original admission cause`,
		},
	} {
		diagnostic, projected := MCPProbeSelectionDiagnostic(fmt.Errorf("selection: %w", test.failure))
		if !projected || diagnostic.HumanText() != test.want {
			t.Fatalf("projection = %t, %q; want %q", projected, diagnostic.HumanText(), test.want)
		}
	}
}

func TestMCPProbeSelectionDiagnosticKeepsSpecializedAdmissionCause(t *testing.T) {
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetOpenCode, target.ScopeProject)
	if !ok {
		t.Fatal("missing OpenCode placement")
	}
	cause := placement.AdmitEnvironmentReference("CHILD_TOKEN", "HOST_TOKEN")
	if cause == nil {
		t.Fatal("environment mapping unexpectedly admitted")
	}
	diagnostic, projected := MCPProbeSelectionDiagnostic(probeworkflow.BindingAdmissionError{Name: "context7", Err: cause})
	want := `mcp-server "context7" runtime-probe admission: OpenCode MCP projection does not support env in the admitted local-command adapter`
	if !projected || diagnostic.HumanText() != want {
		t.Fatalf("projection = %t, %q; want specialized cause %q", projected, diagnostic.HumanText(), want)
	}
}

func TestMCPProbeSelectionDiagnosticUsesSafeFailureBoundary(t *testing.T) {
	for _, failure := range []error{
		probeworkflow.NoAdmittedBindingError{Name: "https://user:literal-secret@example.test/server"},
		probeworkflow.AmbiguousBindingError{Name: "server\n\x1b[2J\u202e", Matches: 2},
		probeworkflow.BindingAdmissionError{Name: "server", Err: errors.New("cause\n\x1b[2J\u202e token=literal-secret")},
	} {
		diagnostic, projected := MCPProbeSelectionDiagnostic(failure)
		if !projected {
			t.Fatalf("selection error not projected: %T", failure)
		}
		text := diagnostic.HumanText()
		for _, forbidden := range []string{"literal-secret", "\n", "\x1b", "\u202e"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("unsafe dynamic detail %q in %q", forbidden, text)
			}
		}
		if !strings.HasPrefix(text, "mcp-server ") {
			t.Fatalf("lost public resource label: %q", text)
		}
	}
}

func TestMCPProbeSelectionDiagnosticDoesNotReinterpretOtherErrors(t *testing.T) {
	for _, failure := range []error{nil, errors.New(`mcp_server "context7" is ambiguous across 2 runtime-probe rows`)} {
		if _, projected := MCPProbeSelectionDiagnostic(failure); projected {
			t.Fatalf("untyped error was reinterpreted: %v", failure)
		}
	}
}
