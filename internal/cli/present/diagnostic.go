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
			fmt.Fprintf(
				output,
				"%s resource=%s target=%s scope=%s: %s",
				Escape(string(diagnostic.Severity)),
				Quote(resourceIDString(diagnostic.EntityID)),
				Escape(string(diagnostic.Target)),
				Escape(string(diagnostic.Scope)),
				Escape(diagnostic.Detail),
			)
			if diagnostic.NextStep != "" {
				fmt.Fprintf(output, " next=%s", Quote(diagnostic.NextStep))
			}
			fmt.Fprintln(output)
			continue
		}
		fmt.Fprintf(
			output,
			"%s code=%s resource=%s target=%s scope=%s event=%s command=%s detail=%s",
			Escape(string(diagnostic.Severity)),
			Escape(diagnostic.Code),
			Quote(resourceIDString(diagnostic.EntityID)),
			Escape(string(diagnostic.Target)),
			Escape(string(diagnostic.Scope)),
			Quote(diagnostic.Event),
			Quote(diagnostic.Command),
			Quote(diagnostic.Detail),
		)
		if diagnostic.Repairability != "" {
			fmt.Fprintf(output, " repairability=%s", Escape(diagnostic.Repairability))
		}
		if diagnostic.NextStep != "" {
			fmt.Fprintf(output, " next=%s", Quote(diagnostic.NextStep))
		}
		fmt.Fprintln(output)
	}
}
