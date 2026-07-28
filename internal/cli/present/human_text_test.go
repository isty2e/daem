package clipresent

import (
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestEscapePreservesPrintableTextAndEscapesTerminalControls(t *testing.T) {
	input := "plain path/한글 \\ quote\"\n\t\x1b[2J\u202e" + string([]byte{0xff})
	want := `plain path/한글 \\ quote"\n\t\x1b[2J\u202e\xff`
	if got := Escape(input); got != want {
		t.Fatalf("Escape = %q, want %q", got, want)
	}
}

func TestErrorHandlesNilAndEscapesDynamicDetail(t *testing.T) {
	if got := Error(nil); got != "" {
		t.Fatalf("Error(nil) = %q, want empty", got)
	}
	if got, want := Error(errors.New("failure\nnext: run injected")), `failure\nnext: run injected`; got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

func TestErrorSpecializesUnsupportedMCPEnvironmentReference(t *testing.T) {
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}
	err := placement.AdmitEnvironmentReference("TOKEN", "HOST_TOKEN")
	if err == nil {
		t.Fatal("Codex project placement admitted environment reference")
	}
	const want = "Codex MCP projection does not support env in the admitted command/args adapter"
	if got := Error(err); got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}

func TestErrorPreservesCodexGlobalSameNameAdmissionDetail(t *testing.T) {
	placement, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeGlobal)
	if !ok {
		t.Fatal("Codex global placement missing")
	}
	err := placement.AdmitEnvironmentReference("TOKEN", "HOST_TOKEN")
	if err == nil {
		t.Fatal("Codex global placement admitted aliased environment reference")
	}
	const want = `MCP placement "codex.global.default-config" supports only same-name environment references; child "TOKEN" selects source "HOST_TOKEN"`
	if got := Error(err); got != want {
		t.Fatalf("Error = %q, want %q", got, want)
	}
}
