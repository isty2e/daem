package journal

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

func requireDocumentEvidenceFields(content []byte, names ...string) error {
	if err := jsonstrict.Validate(content, "document evidence", maximumRecoveryJournalJSONDepth); err != nil {
		return err
	}
	if err := requireRecoveryJSONFields(content, "document evidence", names...); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return err
	}
	for name, value := range fields {
		for _, character := range []byte(name) {
			if character != '_' && (character < 'a' || character > 'z') && (character < '0' || character > '9') {
				return fmt.Errorf("document evidence field must use canonical lower_snake_case")
			}
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("document evidence field %q must not be null", name)
		}
	}
	return nil
}
