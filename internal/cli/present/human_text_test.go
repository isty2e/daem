package clipresent

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/findings"
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

func TestPrintDiagnosticsEscapesEveryDynamicHumanField(t *testing.T) {
	entityID, err := entity.New(entity.KindSkill, "review")
	if err != nil {
		t.Fatal(err)
	}
	diagnostic := findings.Diagnostic{
		Severity:      findings.Severity("warn\nforged"),
		Code:          "skill\u202e.code",
		EntityID:      entityID,
		Target:        target.Target("codex\nforged"),
		Scope:         target.Scope("project\u202e"),
		Event:         "event\nforged",
		Command:       "command\x1b[2J",
		Detail:        "detail\nforged\u202e",
		Repairability: "manual\nforged",
		NextStep:      "next\nforged",
	}
	for _, options := range []HumanOptions{{}, {Verbose: true}} {
		var output bytes.Buffer
		PrintDiagnosticsWithOptions(&output, []findings.Diagnostic{diagnostic}, options)
		text := output.String()
		for _, forbidden := range []string{"\x1b", "\u202e", "warn\nforged", "detail\nforged"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("output contains raw dynamic field %q: %q", forbidden, text)
			}
		}
		for _, want := range []string{`warn\nforged`, `codex\nforged`, `detail\nforged\u202e`} {
			if !strings.Contains(text, want) {
				t.Fatalf("output = %q, want escaped %q", text, want)
			}
		}
	}
}

func TestQuoteEscapesQuotedTerminalFields(t *testing.T) {
	input := "plain \\ quote\"\n\x1b[2J\u202e" + string([]byte{0xff})
	want := `"plain \\ quote\"\n\x1b[2J\u202e\xff"`
	if got := Quote(input); got != want {
		t.Fatalf("Quote = %q, want %q", got, want)
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

type joinedEvidenceError struct {
	children []error
	calls    *int
}

func (err joinedEvidenceError) Error() string {
	*err.calls = *err.calls + 1
	return "aggregate error must not be materialized"
}

func (err joinedEvidenceError) Unwrap() []error { return err.children }

type opaqueEvidenceError struct {
	calls *int
}

func (err opaqueEvidenceError) Error() string {
	*err.calls = *err.calls + 1
	return "private_token=secret"
}

func TestBoundedErrorEvidenceTraversesJoinedLeavesWithoutMaterializingRoot(t *testing.T) {
	rootCalls := 0
	leafCalls := 0
	children := make([]error, 0, maximumErrorEvidenceNodes+8)
	for index := 0; index < maximumErrorEvidenceNodes+8; index++ {
		children = append(children, opaqueEvidenceError{calls: &leafCalls})
	}
	evidence := BoundedErrorEvidence(joinedEvidenceError{
		children: children,
		calls:    &rootCalls,
	}, 512)
	if rootCalls != 0 || leafCalls != 0 {
		t.Fatalf("Error calls = root:%d leaf:%d, want zero", rootCalls, leafCalls)
	}
	if !strings.Contains(evidence, "error evidence omitted") ||
		strings.Contains(evidence, "secret") {
		t.Fatalf("evidence = %q, want bounded omission", evidence)
	}
}

func TestBoundedErrorEvidenceRetainsEagerStandardErrors(t *testing.T) {
	t.Parallel()

	got := BoundedErrorEvidence(errors.New("static typed boundary failure"), 128)
	if got != "static typed boundary failure" {
		t.Fatalf("evidence = %q, want eager standard error", got)
	}
}

type terminalWrappedEvidenceError struct {
	message string
	calls   *int
	cause   error
}

func (err terminalWrappedEvidenceError) Error() string {
	*err.calls = *err.calls + 1
	return "unbounded Error must not be called"
}

func (err terminalWrappedEvidenceError) Unwrap() error { return err.cause }

func (err terminalWrappedEvidenceError) BoundedErrorEvidence(maximumRunes int) (string, bool) {
	if len([]rune(err.message)) <= maximumRunes {
		return err.message, false
	}
	return string([]rune(err.message)[:maximumRunes]), true
}

func TestBoundedErrorEvidenceRetainsTerminalTypedFailure(t *testing.T) {
	t.Parallel()

	calls := 0
	got := BoundedErrorEvidence(terminalWrappedEvidenceError{
		message: "typed boundary failure",
		calls:   &calls,
		cause:   errors.New("wrapped cause must not replace typed evidence"),
	}, 128)
	if calls != 0 {
		t.Fatalf("Error called %d times", calls)
	}
	if got != "typed boundary failure" {
		t.Fatalf("evidence = %q, want terminal typed failure", got)
	}
}

func TestBoundedErrorEvidenceUsesRootedPathProjectionBeforeCause(t *testing.T) {
	t.Parallel()

	failure := rootedpath.NewBoundaryFailure(
		rootedpath.FailureRootReplaced,
		"/Users/alice/private/project",
		"root identity changed",
		errors.New("open /Users/alice/private/project: permission denied"),
	)
	if got := BoundedErrorEvidence(failure, 128); got != string(rootedpath.FailureRootReplaced) {
		t.Fatalf("evidence = %q, want path-neutral rooted failure", got)
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
