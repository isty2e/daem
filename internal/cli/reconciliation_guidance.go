package cli

import (
	"errors"
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func claudeHookProfileDiagnostic(err error) (clipresent.FailureDiagnostic, bool) {
	var mismatch *nativeconfig.ClaudeHookProfileMismatch
	if !errors.As(err, &mismatch) {
		return clipresent.FailureDiagnostic{}, false
	}
	return clipresent.NewFailureDiagnostic(mismatch.Error()).WithGuidance(clipresent.InstructionGuidance(mismatch.RetryInstruction())), true
}

func assessmentFailureDiagnostic(err error) clipresent.FailureDiagnostic {
	if diagnostic, recognized := claudeHookProfileDiagnostic(err); recognized {
		return diagnostic
	}
	return humanFailureDiagnostic(err)
}

func printReconciliationInspectionHint(output io.Writer, manifestPath, requestedManifestPath string, targets targetFlagValues, verbose bool) {
	if !verbose {
		fmt.Fprintln(output, "next: inspect status --verbose with the same --manifest and --target selection")
		return
	}
	var args []string
	for _, value := range targets.strings() {
		args = append(args, "--target", value)
	}
	args = append(args, "--verbose")
	clipresent.PrintManifestCommand(output, "next: inspect with ", []string{"status"}, manifestPath, manifestHintOptions(requestedManifestPath, false), args...)
}
