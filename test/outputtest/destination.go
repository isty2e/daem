// Package outputtest provides canonical Destination fixtures without importing
// higher-level product packages.
package outputtest

import (
	"testing"

	"github.com/isty2e/daem/internal/output"
)

// Parse returns a validated Destination or fails the calling test.
func Parse(t testing.TB, value string) output.Destination {
	t.Helper()
	destination, err := output.Parse(value)
	if err != nil {
		t.Fatalf("output.Parse(%q) returned error: %v", value, err)
	}
	return destination
}
