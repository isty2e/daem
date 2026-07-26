package adopt

import (
	"fmt"
	"strings"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
)

func skippedSummary(skipped []adoptmodel.Skipped) string {
	if len(skipped) == 0 {
		return ""
	}

	parts := make([]string, 0, len(skipped))
	for _, value := range skipped {
		parts = append(parts, fmt.Sprintf("%s: %s", value.LivePath, value.Reason))
	}

	return " (skipped " + strings.Join(parts, ", ") + ")"
}

func scanSummary(scans []adoptmodel.Scan) string {
	if len(scans) == 0 {
		return ""
	}

	parts := make([]string, 0, len(scans))
	for _, scan := range scans {
		parts = append(
			parts,
			fmt.Sprintf(
				"%s: %s entries=%d imported=%d skipped=%d",
				scan.LivePath,
				scan.Status,
				scan.Entries,
				scan.Imported,
				scan.Skipped,
			),
		)
	}

	return " (scanned " + strings.Join(parts, ", ") + ")"
}
