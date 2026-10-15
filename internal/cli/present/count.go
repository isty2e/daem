package clipresent

import "fmt"

// CountLabel inflects the caller's unit without interpreting what was counted.
func CountLabel(count int, singular string, plural string) string {
	unit := plural
	if count == 1 {
		unit = singular
	}
	return fmt.Sprintf("%d %s", count, unit)
}
