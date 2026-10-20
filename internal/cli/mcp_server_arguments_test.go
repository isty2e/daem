package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestMCPInputCollectionAndCanonicalSelection(t *testing.T) {
	var raw mcpInputFlagValues
	for _, value := range []string{"codex", "claude-code", "codex"} {
		if err := raw.Set(value); err != nil {
			t.Fatal(err)
		}
	}
	if raw.String() != "" || fmt.Sprint(raw) != "" || fmt.Sprint(&raw) != "" {
		t.Fatal("raw collector formats submitted values")
	}
	targets, err := mcpInputTargets(raw)
	if err != nil || !slices.Equal(targets, []string{"codex", "claude-code"}) {
		t.Fatalf("canonical target selection changed: %v %v", targets, err)
	}
	scope, err := mcpInputScope(mcpInputFlagValues{values: []string{"global", "global"}})
	if err != nil || scope != "global" {
		t.Fatalf("repeated scope changed: %q %v", scope, err)
	}
	if _, err := mcpInputScope(mcpInputFlagValues{values: []string{"project", "global"}}); err == nil {
		t.Fatal("multiple distinct scopes admitted")
	}

	const marker = "submitted_bad_selection_marker"
	if err := raw.Set(marker); err != nil {
		t.Fatal("collector must not validate inside Set")
	}
	_, err = mcpInputTargets(raw)
	if err == nil || strings.Contains(err.Error(), marker) || !strings.Contains(err.Error(), "--target") {
		t.Fatalf("unsafe or unhelpful target diagnostic: %v", err)
	}
	for _, accepted := range target.SupportedTargets() {
		if !strings.Contains(err.Error(), string(accepted)) {
			t.Fatalf("target diagnostic omitted accepted target %s", accepted)
		}
	}
	_, err = mcpInputScope(mcpInputFlagValues{values: []string{marker}})
	if err == nil || strings.Contains(err.Error(), marker) || !strings.Contains(err.Error(), "--scope") {
		t.Fatalf("unsafe or unhelpful scope diagnostic: %v", err)
	}
	for _, accepted := range target.SupportedScopes() {
		if !strings.Contains(err.Error(), string(accepted)) {
			t.Fatalf("scope diagnostic omitted accepted scope %s", accepted)
		}
	}
}

func TestMCPInputSourceConventionDoesNotNormalize(t *testing.T) {
	for _, accepted := range []string{"A", "_", "UPPER_SOURCE", "SOURCE2"} {
		if !mcpInputSourceName(accepted) {
			t.Fatalf("valid conventional name rejected: %q", accepted)
		}
	}
	for _, refused := range []string{"", "2SOURCE", "source", "Mixed_SOURCE", " SOURCE", "SOURCE ", "${SOURCE}", "SOURCE=VALUE"} {
		assignments, err := mcpInputEnvironment(mcpInputFlagValues{values: []string{"FIRST=UPPER_SOURCE", "CHILD=" + refused}})
		if err == nil || assignments != nil {
			t.Fatalf("invalid source normalized or partial assignments returned: %q, %#v, %v", refused, assignments, err)
		}
	}
}

func TestMCPInputExtraOperandKeepsOrdinalAndRepairWithoutValue(t *testing.T) {
	path := []string{"add", "mcp-server"}
	flags := newCommandFlagSet(path, io.Discard)
	const marker = "extra_operand_private_marker"
	args := []string{"server", "command", marker}
	plan := prepareAuthoringArguments(flags, args, 2)
	_, err := plan.operands()
	if err == nil {
		t.Fatal("extra operand was admitted")
	}
	diagnostic := mcpInputBindingDiagnostic(path, plan, err, args)
	if strings.Contains(diagnostic.HumanText(), marker) || !strings.Contains(diagnostic.Message(), "operand 3") || !strings.Contains(diagnostic.HumanText(), "--arg") {
		t.Fatalf("unsafe or unhelpful extra-operand diagnosis: %s", diagnostic.HumanText())
	}
}
