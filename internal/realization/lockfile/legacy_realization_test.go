package lockfile

import (
	"testing"

	"github.com/isty2e/daem/internal/realization"
)

func mustRealizationDTO(t testing.TB, spec realization.RealizationSpec, present bool) *realizationDTO {
	t.Helper()
	dto, err := realizationToDTO(spec, present)
	if err != nil {
		t.Fatal(err)
	}
	return dto
}
