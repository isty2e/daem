package clipresent

import (
	"fmt"
	"io"

	"github.com/isty2e/daem/internal/findings"
)

func PrintDiagnosticsWithOptions(output io.Writer, diagnostics []findings.Diagnostic, options HumanOptions) {
	if len(diagnostics) == 0 {
		return
	}
	fmt.Fprintf(output, "diagnostics: %d\n", len(diagnostics))
	for _, diagnostic := range diagnostics {
		if !options.Verbose {
			fmt.Fprintf(output, "%s resource=%q target=%s scope=%s: %s", diagnostic.Severity, resourceIDString(diagnostic.EntityID), diagnostic.Target, diagnostic.Scope, diagnostic.Detail)
			if diagnostic.NextStep != "" {
				fmt.Fprintf(output, " next=%q", diagnostic.NextStep)
			}
			fmt.Fprintln(output)
			continue
		}
		fmt.Fprintf(
			output,
			"%s code=%s resource=%q target=%s scope=%s event=%q command=%q detail=%q",
			diagnostic.Severity,
			diagnostic.Code,
			resourceIDString(diagnostic.EntityID),
			diagnostic.Target,
			diagnostic.Scope,
			diagnostic.Event,
			diagnostic.Command,
			diagnostic.Detail,
		)
		if diagnostic.Repairability != "" {
			fmt.Fprintf(output, " repairability=%s", diagnostic.Repairability)
		}
		if diagnostic.NextStep != "" {
			fmt.Fprintf(output, " next=%q", diagnostic.NextStep)
		}
		fmt.Fprintln(output)
	}
}
