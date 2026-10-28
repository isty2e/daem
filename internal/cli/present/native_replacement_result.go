package clipresent

import (
	"bytes"
	"fmt"
	"strings"

	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
)

func NativeReplacementResultText(disclosure applyworkflow.NativeReplacementDisclosure) ([]byte, error) {
	var written, adopted, recorded []string
	for _, row := range disclosure.Keys {
		key := nativeKeyDisplay(row.Path)
		switch row.Action {
		case "create", "recreate", "takeover", "replace":
			written = append(written, key)
		case "adopt":
			adopted = append(adopted, key)
		case "record":
			recorded = append(recorded, key)
		case "current":
		default:
			return nil, fmt.Errorf("replacement result has an unsupported action")
		}
	}

	var result bytes.Buffer
	if len(written) != 0 {
		fmt.Fprintf(&result, "Applied declared values to %s: %s\n", CountLabel(len(written), "host configuration key", "host configuration keys"), strings.Join(written, ", "))
	}
	if len(adopted) != 0 {
		values := "their values"
		if len(adopted) == 1 {
			values = "its value"
		}
		fmt.Fprintf(&result, "Adopted %s without changing %s: %s\n", CountLabel(len(adopted), "host configuration key", "host configuration keys"), values, strings.Join(adopted, ", "))
	}
	if len(recorded) != 0 {
		fmt.Fprintf(&result, "Updated management records for %s without changing host values: %s\n", CountLabel(len(recorded), "host configuration key", "host configuration keys"), strings.Join(recorded, ", "))
	}
	if len(written)+len(adopted)+len(recorded) == 0 {
		fmt.Fprintln(&result, "No changes were needed; selected host configuration keys are already current.")
	}
	return result.Bytes(), nil
}
