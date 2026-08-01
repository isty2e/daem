package observe

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestOwnershipObservationRejectsProjectDestination(t *testing.T) {
	destination, err := output.Parse(".agents/skills/example")
	if err != nil {
		t.Fatal(err)
	}
	exact, err := pathauthority.NewExact(filepath.Join(t.TempDir(), "example"), "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	address, err := ownership.NewManagedAddress(exact, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewExactOwnershipObservation(
		destination,
		"",
		address,
		ownership.NoClaim(),
	); err == nil {
		t.Fatal("ownership observation accepted a project-local destination")
	}
}
