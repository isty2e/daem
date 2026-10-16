package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/workflow/authoring"
)

func TestMissingMCPHintRequiresOwnedMissingSelection(t *testing.T) {
	_, unknown := declaration.DecodeManifest([]byte("version=1\n\"mcp_server resource not found\"=true\n"))
	if unknown == nil {
		t.Fatal("unknown-key fixture was admitted")
	}
	for _, cause := range []error{
		nil,
		errors.New("mcp_server resource not found"),
		unknown,
		&authoring.ResourceSelectionNotFoundError{Kind: "skill", Name: "missing"},
	} {
		var output bytes.Buffer
		printRemoveMCPServerNotFoundHint(&output, cause)
		if output.Len() != 0 {
			t.Errorf("unrelated failure gained MCP guidance: %v -> %q", cause, output.String())
		}
	}

	var output bytes.Buffer
	printRemoveMCPServerNotFoundHint(&output, &authoring.ResourceSelectionNotFoundError{Kind: "mcp_server", Name: "missing"})
	if output.Len() == 0 {
		t.Fatal("owned MCP selection failure lost its repair guidance")
	}
}

func TestTargetSyntaxHintRequiresUnexpectedArgumentFact(t *testing.T) {
	args := []string{"--target", "codex", "claude-code", "source"}
	var output bytes.Buffer
	if printUnexpectedArgumentTargetHintFromArgs(&output, errors.New("unexpected argument in an unrelated source"), args) || output.Len() != 0 {
		t.Fatalf("unrelated error gained argument repair: %q", output.String())
	}

	_, _, cause := splitAuthoringArgs(args, 1, func(name string) bool { return name == "target" })
	if cause == nil {
		t.Fatal("fixture did not reject the extra argument")
	}
	if !printUnexpectedArgumentTargetHintFromArgs(&output, cause, args) || output.Len() == 0 {
		t.Fatal("actual unexpected argument lost its target-list repair")
	}
}
