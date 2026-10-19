package clipresent

import (
	"fmt"
	"io"
)

func summarizeUnchanged[T any](rows []T, verbose bool, unchanged func(T) bool) ([]T, int) {
	if verbose {
		return rows, 0
	}

	var visible []T
	omitted := 0
	for _, row := range rows {
		if unchanged(row) {
			omitted++
			continue
		}
		visible = append(visible, row)
	}
	return visible, omitted
}

func printSectionSummary(output io.Writer, title string, count string, unchanged int) {
	fmt.Fprintf(output, "%s: %s", title, count)
	if unchanged != 0 {
		fmt.Fprintf(output, " (%d unchanged)", unchanged)
	}
	fmt.Fprintln(output)
}
