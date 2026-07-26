package hostroute

import "testing"

func TestDurableResultClassRejectsUnreachableClass(t *testing.T) {
	for _, class := range []ResultClass{"", "unsupported", "history_only"} {
		if _, err := durableResultClass(class); err == nil {
			t.Fatalf("durableResultClass(%q) returned nil error", class)
		}
	}
}
