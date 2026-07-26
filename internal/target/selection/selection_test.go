package targetselection

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestForAvailableTargetsSelectsAllAvailableTargetsInStableOrder(t *testing.T) {
	selection, err := ForAvailableTargets([]target.Target{
		target.TargetPi,
		target.TargetClaudeCode,
		target.TargetCodex,
		target.TargetCodex,
	}, nil)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	assertTargets(t, selection.Targets(), []target.Target{
		target.TargetCodex,
		target.TargetClaudeCode,
		target.TargetPi,
	})
}

func TestForAvailableTargetsReturnsEmptySelectionWhenNoTargetsAreAvailable(t *testing.T) {
	selection, err := ForAvailableTargets(nil, nil)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	assertTargets(t, selection.Targets(), nil)
	if selection.Includes(target.TargetCodex) {
		t.Fatal("selection includes codex")
	}
}

func TestForAvailableTargetsAcceptsRepeatedSelectedTargets(t *testing.T) {
	selection, err := ForAvailableTargets(
		[]target.Target{target.TargetCodex, target.TargetClaudeCode},
		[]string{"claude-code", " codex ", "codex"},
	)
	if err != nil {
		t.Fatalf("ForAvailableTargets returned error: %v", err)
	}

	assertTargets(t, selection.Targets(), []target.Target{
		target.TargetCodex,
		target.TargetClaudeCode,
	})

	if !selection.Includes(target.TargetCodex) {
		t.Fatal("selection does not include codex")
	}
	if selection.Includes(target.TargetPi) {
		t.Fatal("selection includes pi")
	}
	if !selection.IncludesAny([]target.Target{target.TargetPi, target.TargetClaudeCode}) {
		t.Fatal("selection does not include any candidate")
	}
	if selection.IncludesAny([]target.Target{target.TargetPi, target.TargetOpenCode}) {
		t.Fatal("selection includes an unselected candidate")
	}
	if selection.IncludesAny(nil) {
		t.Fatal("selection includes an empty candidate set")
	}
}

func TestForAvailableTargetsRejectsUnsplitCommaSeparatedSelectedTargets(t *testing.T) {
	_, err := ForAvailableTargets(
		[]target.Target{target.TargetCodex, target.TargetClaudeCode, target.TargetPi},
		[]string{"claude-code, codex"},
	)
	if err == nil {
		t.Fatal("ForAvailableTargets returned nil error")
	}
	if !strings.Contains(err.Error(), `unknown target "claude-code, codex"`) {
		t.Fatalf("error = %q, want unsplit target diagnostic", err.Error())
	}
}

func TestForAvailableTargetsRejectsUnsupportedAvailableTarget(t *testing.T) {
	_, err := ForAvailableTargets([]target.Target{"unknown-agent"}, nil)
	if err == nil {
		t.Fatal("ForAvailableTargets returned nil error")
	}

	if !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("error = %q, want unknown target diagnostic", err.Error())
	}
}

func TestForAvailableTargetsRejectsSupportedTargetWithoutAvailability(t *testing.T) {
	_, err := ForAvailableTargets([]target.Target{target.TargetCodex}, []string{"claude-code"})
	if err == nil {
		t.Fatal("ForAvailableTargets returned nil error")
	}

	if !strings.Contains(err.Error(), "does not match any manifest resource") {
		t.Fatalf("error = %q, want manifest resource diagnostic", err.Error())
	}
}

func TestForAvailableTargetsRejectsRequestedTargetWhenNoTargetsAreAvailable(t *testing.T) {
	_, err := ForAvailableTargets(nil, []string{"codex"})
	if err == nil {
		t.Fatal("ForAvailableTargets returned nil error")
	}

	if !strings.Contains(err.Error(), `target "codex" does not match any manifest resource`) {
		t.Fatalf("error = %q, want unavailable target diagnostic", err.Error())
	}
}

func TestForAvailableTargetsRejectsUnsupportedTarget(t *testing.T) {
	_, err := ForAvailableTargets([]target.Target{target.TargetCodex}, []string{"unknown-agent"})
	if err == nil {
		t.Fatal("ForAvailableTargets returned nil error")
	}

	if !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("error = %q, want unknown target diagnostic", err.Error())
	}
}

func TestForAvailableTargetsRejectsWhitespaceOnlyTarget(t *testing.T) {
	_, err := ForAvailableTargets([]target.Target{target.TargetCodex}, []string{" \t "})
	if err == nil {
		t.Fatal("ForAvailableTargets returned nil error")
	}

	if !strings.Contains(err.Error(), `unknown target ""`) {
		t.Fatalf("error = %q, want empty target diagnostic", err.Error())
	}
}

func TestForDiagnosticsSelectsAllSupportedTargetsByDefault(t *testing.T) {
	selection, err := ForDiagnostics(nil)
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	assertTargets(t, selection.Targets(), []target.Target{
		target.TargetCodex,
		target.TargetClaudeCode,
		target.TargetOpenCode,
		target.TargetPi,
		target.TargetAntigravityCLI,
	})
}

func TestForDiagnosticsAcceptsSupportedTargetWithoutManifestResource(t *testing.T) {
	selection, err := ForDiagnostics([]string{"pi", "pi"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	assertTargets(t, selection.Targets(), []target.Target{target.TargetPi})
}

func TestForDiagnosticsRejectsUnsupportedTarget(t *testing.T) {
	_, err := ForDiagnostics([]string{"unknown-agent"})
	if err == nil {
		t.Fatal("ForDiagnostics returned nil error")
	}

	if !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("error = %q, want unknown target diagnostic", err.Error())
	}
}

func TestForDiagnosticsRejectsUnsplitCommaSeparatedTarget(t *testing.T) {
	_, err := ForDiagnostics([]string{"codex,unknown-agent"})
	if err == nil {
		t.Fatal("ForDiagnostics returned nil error")
	}

	if !strings.Contains(err.Error(), `unknown target "codex,unknown-agent"`) {
		t.Fatalf("error = %q, want unsplit target diagnostic", err.Error())
	}
}

func TestSelectionTargetsReturnsCopy(t *testing.T) {
	selection, err := ForDiagnostics([]string{"codex"})
	if err != nil {
		t.Fatalf("ForDiagnostics returned error: %v", err)
	}

	targets := selection.Targets()
	targets[0] = target.TargetPi

	assertTargets(t, selection.Targets(), []target.Target{target.TargetCodex})
}

func assertTargets(t *testing.T, got []target.Target, want []target.Target) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("targets = %#v, want %#v", got, want)
		}
	}
}
