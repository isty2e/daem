package journal

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func TestRecoveryJournalV15RefusesPriorFingerprintGenerationBeforeStateDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	content := []byte(legacyV14StateV9Golden)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	codec := &countingStateGenerationCodec{SnapshotCodec: testStateCodec()}
	if _, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, codec); err == nil {
		t.Fatal("old journal authority generation was adopted by the new state encoder")
	}
	if codec.decodes != 0 {
		t.Fatal("old journal was reinterpreted before refusing its authority generation")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, content) {
		t.Fatalf("refusing the old authority generation rewrote the artifact: %v", err)
	}
}

type countingStateGenerationCodec struct {
	durable.SnapshotCodec
	decodes int
}

func (codec *countingStateGenerationCodec) Decode(content []byte) (durable.Snapshot, error) {
	codec.decodes++
	return codec.SnapshotCodec.Decode(content)
}

func setNestedStateField(t testing.TB, content json.RawMessage, name string, value json.RawMessage) json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	fields[name] = value
	encoded, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
