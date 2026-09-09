package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/workflow/authoring"
)

func TestPrintAuthoringOperationErrorMissingSelectionHintQuotesSelectors(t *testing.T) {
	failure := authoring.OperationError{Phase: authoring.OperationPhaseBuildManifestChange, Err: fmt.Errorf("wrapped: %w", missingSelectionForTest())}
	var output strings.Builder
	printAuthoringOperationError(&output, "remove", "/tmp/a'b/manifest.toml", failure)
	want := "hint: available selection: daem remove instruction 'project name' --manifest '/tmp/a'\"'\"'b/manifest.toml' --target pi --target 'codex agent' --scope global\n"
	if !strings.Contains(output.String(), want) {
		t.Fatalf("output = %q, want substring %q", output.String(), want)
	}
}

func TestPrintAuthoringOperationErrorWithoutSelectionHasNoHint(t *testing.T) {
	var output strings.Builder
	printAuthoringOperationError(&output, "remove", "manifest.toml", fmt.Errorf("plain failure"))
	if strings.Contains(output.String(), "hint: available selection:") {
		t.Fatalf("unexpected hint: %q", output.String())
	}
}

func missingSelectionForTest() error {
	return fmt.Errorf("inner: %w", &authoring.ResourceSelectionNotFoundError{
		Kind: "instructions", Name: "missing",
		Available: []authoring.ResourceSelection{{Name: "project name", Scope: "global", Targets: []string{"pi", "codex agent"}}},
	})
}
