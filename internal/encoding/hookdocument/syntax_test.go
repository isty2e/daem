package hookdocument

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func TestValidateClassifiesIngressFailures(t *testing.T) {
	duplicateErr := Validate([]byte(`{"hooks":{},"hooks":{}}`))
	if !errors.Is(duplicateErr, jsonstrict.ErrDuplicateObjectKey) {
		t.Fatalf("duplicate error = %v, want ErrDuplicateObjectKey", duplicateErr)
	}
	depthErr := Validate([]byte(
		strings.Repeat("[", MaximumDepth+2) +
			"0" +
			strings.Repeat("]", MaximumDepth+2),
	))
	if !errors.Is(depthErr, jsonstrict.ErrMaximumDepthExceeded) {
		t.Fatalf("depth error = %v, want ErrMaximumDepthExceeded", depthErr)
	}
	oversizedErr := Validate(make([]byte, MaximumBytes+1))
	if !errors.Is(oversizedErr, ErrTooLarge) {
		t.Fatalf("oversized error = %v, want ErrTooLarge", oversizedErr)
	}
}
