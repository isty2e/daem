package cli

import (
	"fmt"
	"io"

	clipresent "github.com/isty2e/daem/internal/cli/present"
)

func printReconciliationInspectionHint(output io.Writer, manifestPath string, targets targetFlagValues, verbose bool) {
	if !verbose {
		fmt.Fprintln(output, "next: inspect status --verbose with the same --manifest and --target selection")
		return
	}
	args := []string{"daem", "status", "--manifest", manifestPath}
	for _, value := range targets.strings() {
		args = append(args, "--target", value)
	}
	args = append(args, "--verbose")
	clipresent.PrintShellCommand(output, "next: inspect with ", args...)
}
