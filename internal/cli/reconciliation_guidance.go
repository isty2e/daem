package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

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
