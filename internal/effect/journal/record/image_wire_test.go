package record

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestDocumentImageWireRejectsMissingMixedAndDuplicateEvidence(t *testing.T) {
	for _, content := range []string{
		`{}`, `null`, `{"kind":"absent","mode":null}`, `{"kind":"absent","mode":0}`,
		`{"kind":"absent","artifact":null}`, `{"kind":"absent","extra":true}`,
		`{"kind":"absent","Mode":null}`, `{"kind":"absent","Kind":"absent"}`,
		`{"kind":"absent","kind":"file"}`, `{"kind":"file","mode":0}`,
		`{"kind":"file","mode":0,"artifact":{"path":"images/before","digest":"sha256:00"}}`,
	} {
		var decoded recoveryDocumentImage
		if err := json.Unmarshal([]byte(content), &decoded); err == nil {
			t.Fatalf("invalid closed image record accepted: %s", content)
		}
	}
	mode := recovery.PermissionMode(0o600)
	for _, path := range []string{"", "../outside", "/outside", "images/../before"} {
		if _, err := persistedDocumentImage(aggregate.ExistingDocument(nil), &mode, path); err == nil {
			t.Fatal("image accepted an escaped or noncanonical artifact reference")
		}
	}
	if _, err := persistedDocumentImage(aggregate.AbsentDocument(), nil, "images/before"); err == nil {
		t.Fatal("absent image retained an artifact")
	}
}
