package hookdocument

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestStructuralValidationStopsAtCardinalityLimits(t *testing.T) {
	const entries = 100_000
	events := make([]string, entries)
	for index := range events {
		events[index] = fmt.Sprintf(`"event-%d":[]`, index)
	}
	handlers := strings.Repeat(`{},`, entries-1) + `{}`
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "events", content: `{"hooks":{` + strings.Join(events, ",") + `}}`},
		{name: "groups", content: `{"hooks":{"AnyEvent":[` + handlers + `]}}`},
		{name: "handlers", content: `{"hooks":{"AnyEvent":[{"hooks":[` + handlers + `]}]}}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := strings.NewReader(test.content)
			err := validateDocumentStructureReader(reader)
			if !errors.Is(err, ErrStructuralBudgetExceeded) {
				t.Fatalf("validateDocumentStructureReader error = %v, want structural budget error", err)
			}
			if consumed := len(test.content) - reader.Len(); consumed >= len(test.content)/2 {
				t.Fatalf("structural validation consumed %d of %d bytes, want early stop", consumed, len(test.content))
			}
		})
	}
}

func TestValidateProjectionEnforcesSharedHandlerBudget(t *testing.T) {
	handlers := strings.Repeat(`{"type":"command","command":"true"},`, MaximumHandlers) +
		`{"type":"command","command":"true"}`
	content := []byte(`{"Stop":[{"hooks":[` + handlers + `]}]}`)
	if err := ValidateProjection(content); !errors.Is(err, ErrStructuralBudgetExceeded) {
		t.Fatalf("ValidateProjection error = %v, want structural budget error", err)
	}
}
