package journal

import "github.com/isty2e/daem/internal/encoding/jsonstrict"

func requireDocumentEvidenceFields(content []byte, names ...string) error {
	if err := jsonstrict.ValidateRecord(content, "document evidence", maximumRecoveryJournalJSONDepth); err != nil {
		return err
	}
	return requireRecoveryJSONFields(content, "document evidence", names...)
}
