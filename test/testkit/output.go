package testkit

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func parseDestination(t testing.TB, value string) output.Destination {
	t.Helper()
	destination, err := output.Parse(value)
	if err != nil {
		t.Fatalf("output.Parse(%q) returned error: %v", value, err)
	}
	return destination
}
