package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRecoveryJournalValidatesExactBeforeAndAfterIdentities(t *testing.T) {
	entry := defaultRecoveryEntry()
	beforeEntry := recoveryEntryFor(
		"project",
		"CLAUDE.md",
		entry.StateBefore.ContentHash,
		entry.StateBefore.ContentHash,
		entry.Before.BackupPath,
	)
	beforeIdentity := recoveryStateIdentityFromEntry(beforeEntry)
	entry.StateBeforeIdentity = &beforeIdentity
	journal := recoveryJournalFor(entry)
	before := resourceState(beforeEntry, entry.StateBefore.ContentHash)
	journal.StatefileBefore = statefileFor(before)

	content, err := marshalRecoveryJournal(journal, testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	if !bytes.Contains(content, []byte(`"state_before_identity"`)) {
		t.Fatalf("journal = %s, want exact state_before_identity", content)
	}

	journal.Entries[0].StateBeforeIdentity = nil
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), "statefile_before") {
		t.Fatalf("error = %v, want exact before identity validation failure", err)
	}
}

func TestRecoveryJournalOmitsRedundantBeforeIdentity(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	if bytes.Contains(content, []byte(`"state_before_identity"`)) {
		t.Fatalf("unchanged-identity journal unexpectedly changed shape: %s", content)
	}
}

func TestRecoveryJournalWritesSubjectWithoutLegacyResourceField(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}

	var persisted struct {
		Entries []map[string]json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("unmarshal recovery journal: %v", err)
	}
	if len(persisted.Entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(persisted.Entries))
	}
	if _, present := persisted.Entries[0]["resource"]; present {
		t.Fatalf("journal entry retained forbidden legacy resource field: %s", content)
	}
	if _, present := persisted.Entries[0]["subject"]; !present {
		t.Fatalf("current subject-owned journal entry omitted subject: %s", content)
	}
}

func TestRecoveryJournalRejectsVersionSixResourceIdentity(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	content = bytes.Replace(content, []byte(`"version": 7`), []byte(`"version": 6`), 1)
	content = bytes.Replace(
		content,
		[]byte(`"subject": {`),
		[]byte(`"resource": {"kind": "hook", "name": "format"}, "subject": {`),
		1,
	)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "journal.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecoveryJournal(context.Background(), journalTestFilesystem(), path, testStateCodec()); err == nil ||
		!strings.Contains(err.Error(), "unsupported recovery journal version 6") {
		t.Fatalf("loadRecoveryJournal error = %v, want version-6 rejection", err)
	}
}

func TestRecoveryJournalRejectsResourceFieldInVersionSeven(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	content = bytes.Replace(
		content,
		[]byte(`"subject": {`),
		[]byte(`"resource": {"kind": "hook", "name": "format"}, "subject": {`),
		1,
	)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "journal.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecoveryJournal(context.Background(), journalTestFilesystem(), path, testStateCodec()); err == nil ||
		!strings.Contains(err.Error(), `unknown field "resource"`) {
		t.Fatalf("loadRecoveryJournal error = %v, want legacy resource-field rejection", err)
	}
}

func TestRecoveryJournalPreservesManagedPathConsumerSetTransition(t *testing.T) {
	entry := managedPathRecoveryEntry()
	beforeIdentity := recoveryStateIdentityFromEntry(entry)
	beforeIdentity.Targets = []string{"antigravity-cli", "codex"}
	entry.StateBeforeIdentity = &beforeIdentity
	journal := recoveryJournalFor(entry)
	before := resourceState(entry, entry.StateBefore.ContentHash)
	beforeEntry := entry
	beforeEntry.Targets = append([]string(nil), beforeIdentity.Targets...)
	before = resourceState(beforeEntry, entry.StateBefore.ContentHash)
	journal.StatefileBefore = statefileFor(before)

	content, err := marshalRecoveryJournal(journal, testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	var roundTrip recoveryJournal
	if err := json.Unmarshal(content, &roundTrip); err != nil {
		t.Fatalf("unmarshal recovery journal: %v", err)
	}
	if got := roundTrip.Entries[0].StateBeforeIdentity.Targets; !slices.Equal(got, beforeIdentity.Targets) {
		t.Fatalf("previous consumer set = %v, want %v", got, beforeIdentity.Targets)
	}

	journal.Entries[0].StateBeforeIdentity = nil
	if err := validateRecoveryJournal(journal, testStateCodec()); err == nil || !strings.Contains(err.Error(), "consumers") {
		t.Fatalf("missing before consumer identity error = %v, want rejection", err)
	}
}

func TestValidateRecoveryJournalRejectsLegacyModeLessSchema(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.Version = 2

	err := validateRecoveryJournal(journal, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery journal version 2") {
		t.Fatalf("error = %v, want legacy schema rejection", err)
	}
}

func TestLoadRecoveryJournalRejectsInvalidNestedStateSchemas(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	var persisted recoveryJournalDTO
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatal(err)
	}
	validBefore := append(json.RawMessage(nil), persisted.StatefileBefore...)

	tests := []struct {
		name   string
		mutate func(*recoveryJournalDTO)
		want   string
	}{
		{
			name: "old journal version",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.Version = 5
			},
			want: "unsupported recovery journal version 5",
		},
		{
			name: "old nested state version",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					strings.Replace(string(validBefore), `"version": 7`, `"version": 1`, 1),
				)
			},
			want: "statefile_before: unsupported statefile version 1",
		},
		{
			name: "missing nested state families",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(`{"version":7}`)
			},
			want: "requires every durable fact-family array",
		},
		{
			name: "duplicate nested state key",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					strings.Replace(string(validBefore), `"version": 7`, `"version": 7, "version": 7`, 1),
				)
			},
			want: "duplicate object key",
		},
		{
			name: "null nested state",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(`null`)
			},
			want: "unsupported statefile version 0",
		},
		{
			name: "unknown nested state field",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					strings.Replace(string(validBefore), `"version": 7`, `"version": 7, "ready": true`, 1),
				)
			},
			want: "unknown field",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := persisted
			candidate.StatefileBefore = append(json.RawMessage(nil), persisted.StatefileBefore...)
			candidate.StatefileAfter = append(json.RawMessage(nil), persisted.StatefileAfter...)
			test.mutate(&candidate)
			mutated, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			directory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "journal.json")
			if err := os.WriteFile(path, mutated, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRecoveryJournal(
				context.Background(),
				journalTestFilesystem(),
				path,
				testStateCodec(),
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadRecoveryJournal error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadRecoveryJournalRejectsDuplicateKeys(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "top level",
			content: strings.Replace(
				string(content),
				`"version": 7`,
				`"version": 7, "version": 7`,
				1,
			),
		},
		{
			name: "nested entry",
			content: strings.Replace(
				string(content),
				`"before": {`,
				`"before": {"existed": true,`,
				1,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "journal.json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec()); err == nil ||
				!strings.Contains(err.Error(), "duplicate object key") {
				t.Fatalf("loadRecoveryJournal error = %v, want duplicate-key rejection", err)
			}
		})
	}
}

func TestLoadRecoveryJournalRejectsOversizedJournal(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "journal.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((64 << 20) + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec()); err == nil ||
		!strings.Contains(err.Error(), "exceeds 67108864 bytes") {
		t.Fatalf("loadRecoveryJournal error = %v, want bounded journal rejection", err)
	}
}

func TestLoadRecoveryJournalRejectsMalformedJSONEnvelope(t *testing.T) {
	valid, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		content []byte
		want    string
	}{
		{
			name:    "invalid UTF-8",
			content: []byte{0xff},
			want:    "not valid UTF-8",
		},
		{
			name:    "multiple values",
			content: append(append([]byte(nil), valid...), []byte(`{}`)...),
			want:    "multiple JSON values",
		},
		{
			name: "excessive depth",
			content: []byte(
				strings.Repeat("[", maximumRecoveryJournalJSONDepth+2) +
					strings.Repeat("]", maximumRecoveryJournalJSONDepth+2),
			),
			want: "exceeds maximum depth 64",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			directory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "journal.json")
			if err := os.WriteFile(path, test.content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec()); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadRecoveryJournal error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLoadRecoveryJournalRejectsMalformedContentHashes(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*recoveryJournalDTO)
		want   string
	}{
		{
			name: "before path hash",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.Entries[0].Before.ContentHash = "sha256:short"
			},
			want: "before.content_hash",
		},
		{
			name: "expected path hash",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.Entries[0].ExpectedAfter.ContentHash = strings.ToUpper(
					candidate.Entries[0].ExpectedAfter.ContentHash,
				)
			},
			want: "expected_after.content_hash",
		},
		{
			name: "before membership hash",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.Entries[0].StateBefore.ContentHash = "sha512:" + strings.Repeat("a", 64)
			},
			want: "state_before: managed state content hash",
		},
		{
			name: "expected membership hash",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.Entries[0].StateExpectedAfter.ContentHash = "sha256:" + strings.Repeat("a", 63)
			},
			want: "state_expected_after: managed state content hash",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var candidate recoveryJournalDTO
			if err := json.Unmarshal(content, &candidate); err != nil {
				t.Fatal(err)
			}
			test.mutate(&candidate)
			mutated, err := json.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			directory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "journal.json")
			if err := os.WriteFile(path, mutated, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadRecoveryJournal(
				context.Background(),
				journalTestFilesystem(),
				path,
				testStateCodec(),
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("loadRecoveryJournal error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestMarshalRecoveryJournalOwnsStateEncoderBytes(t *testing.T) {
	journal := defaultRecoveryJournal()
	encoder := &reusingBufferStateEncoder{buffer: make([]byte, 0, 1<<20)}

	content, err := marshalRecoveryJournal(journal, encoder)
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	var persisted recoveryJournalDTO
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("decode recovery journal DTO: %v", err)
	}
	before, err := testStateCodec().Decode(persisted.StatefileBefore)
	if err != nil {
		t.Fatalf("decode statefile_before: %v", err)
	}
	after, err := testStateCodec().Decode(persisted.StatefileAfter)
	if err != nil {
		t.Fatalf("decode statefile_after: %v", err)
	}
	if !before.Equal(journal.StatefileBefore) {
		t.Fatal("statefile_before was overwritten by the encoder's reused buffer")
	}
	if !after.Equal(journal.StatefileAfter) {
		t.Fatal("statefile_after differs from the encoded journal snapshot")
	}
}

func TestLoadRecoveryJournalRetainsEvidenceOnStateDecodingFailure(t *testing.T) {
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, recoveryJournalFileName)
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}
	codecErr := fmt.Errorf("injected state decoding failure")

	_, err = loadRecoveryJournal(
		context.Background(),
		journalTestFilesystem(),
		path,
		failingStateCodec{decodeErr: codecErr},
	)
	if !errors.Is(err, codecErr) {
		t.Fatalf("loadRecoveryJournal error = %v, want state decoding failure", err)
	}
	retained, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read retained journal: %v", readErr)
	}
	if !bytes.Equal(retained, content) {
		t.Fatal("state decoding failure changed journal evidence")
	}
}
