package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
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

func TestRecoveryJournalRequiresScopeExactGlobalPathBinding(t *testing.T) {
	global := globalAcquireRecoveryEntry(t)
	global.GlobalPathBinding = nil
	if err := validateRecoveryEntries([]recoveryEntry{global}); err == nil ||
		!strings.Contains(err.Error(), "global entry requires its capture-time path binding") {
		t.Fatalf("missing global path binding error = %v", err)
	}

	global.GlobalPathBinding = testRecoveryGlobalPathBinding("relative/path")
	if err := validateRecoveryEntries([]recoveryEntry{global}); err == nil ||
		!strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("relative global path binding error = %v", err)
	}
	global.GlobalPathBinding = testRecoveryGlobalPathBinding(string([]byte{'/', 0xff}))
	if err := validateRecoveryEntries([]recoveryEntry{global}); err == nil ||
		!strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("non-UTF-8 global path binding error = %v", err)
	}

	resolvedPath := filepath.Join(t.TempDir(), "daem-global-config.json")
	global.GlobalPathBinding = testRecoveryGlobalPathBinding(resolvedPath)
	if err := validateRecoveryEntries([]recoveryEntry{global}); err != nil {
		t.Fatalf("exact global path binding rejected: %v", err)
	}

	global.GlobalPathBinding.RootProvenance.ObjectFingerprint = "sha256:short"
	if err := validateRecoveryEntries([]recoveryEntry{global}); err == nil ||
		!strings.Contains(err.Error(), "object fingerprint is invalid") {
		t.Fatalf("invalid global root provenance error = %v", err)
	}

	project := defaultRecoveryEntry()
	project.GlobalPathBinding = testRecoveryGlobalPathBinding(
		filepath.Join(t.TempDir(), "forged-project-path"),
	)
	if err := validateRecoveryEntries([]recoveryEntry{project}); err == nil ||
		!strings.Contains(err.Error(), "project entry must not carry") {
		t.Fatalf("project global path binding error = %v", err)
	}
}

func TestRecoveryGlobalPathBindingWireShapeIsCohesive(t *testing.T) {
	entry := globalAcquireRecoveryEntry(t)
	entry.GlobalPathBinding = &recoveryGlobalPathBinding{
		ResolvedPath: "/test/home/.codex/AGENTS.md",
		RootProvenance: recoveryRootProvenance{
			PhysicalRoot:      "/test/home",
			ObjectFingerprint: "sha256:" + strings.Repeat("3", 64),
			MountFingerprint:  "sha256:" + strings.Repeat("4", 64),
		},
	}
	content, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal recovery entry: %v", err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatalf("decode recovery entry: %v", err)
	}
	if _, legacy := document["resolved_global_path"]; legacy {
		t.Fatal("global path binding leaked the obsolete scalar field")
	}
	rawBinding, present := document["global_path_binding"]
	if !present {
		t.Fatal("global_path_binding is missing")
	}
	var binding map[string]json.RawMessage
	if err := json.Unmarshal(rawBinding, &binding); err != nil {
		t.Fatalf("decode global_path_binding: %v", err)
	}
	if len(binding) != 2 || binding["resolved_path"] == nil || binding["root_provenance"] == nil {
		t.Fatalf("global_path_binding keys = %v, want resolved_path and root_provenance", slices.Sorted(maps.Keys(binding)))
	}
	var provenance map[string]json.RawMessage
	if err := json.Unmarshal(binding["root_provenance"], &provenance); err != nil {
		t.Fatalf("decode root_provenance: %v", err)
	}
	if len(provenance) != 3 || provenance["physical_root"] == nil ||
		provenance["object_fingerprint"] == nil || provenance["mount_fingerprint"] == nil {
		t.Fatalf("root_provenance keys = %v", slices.Sorted(maps.Keys(provenance)))
	}
}

func TestRecoveryJournalRejectsVersionSix(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatalf("marshalRecoveryJournal returned error: %v", err)
	}
	content = bytes.Replace(content, []byte(`"version": 10`), []byte(`"version": 6`), 1)
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

func TestRecoveryJournalRejectsPreAuthorityVersionNine(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte(`"version": 10`), []byte(`"version": 9`), 1)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, recoveryJournalFileName)
	if err := os.WriteFile(path, content, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}
	_, err = loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery journal version 9") ||
		!strings.Contains(err.Error(), "recover before upgrading") {
		t.Fatalf("loadRecoveryJournal error = %v, want pre-v10 retirement guidance", err)
	}
}

func TestRecoveryJournalClassifiesFutureVersionBeforeStrictSchema(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(content, []byte(`"version": 10`), []byte(`"version": 11, "future": true`), 1)
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "journal.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "written by a newer daem") {
		t.Fatalf("loadRecoveryJournal error = %v, want newer-daem guidance", err)
	}
	if strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("future recovery journal was decoded as current schema: %v", err)
	}
}

func TestRecoveryJournalRejectsCaseVariantCurrentFields(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.Replace(
		content,
		[]byte(`"entries": [`),
		[]byte(`"ENTRIES": [], "entries": [`),
		1,
	)
	if !bytes.Contains(content, []byte(`"ENTRIES"`)) {
		t.Fatal("test mutation did not add case-variant field")
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "journal.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec())
	if err == nil || !strings.Contains(err.Error(), "ASCII lower_snake_case") {
		t.Fatalf("loadRecoveryJournal error = %v, want canonical-field rejection", err)
	}
}

func TestRecoveryJournalRejectsResourceFieldInVersionEight(t *testing.T) {
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
			name: "pre-authority-witness journal version",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.Version = 7
			},
			want: "unsupported recovery journal version 7",
		},
		{
			name: "old nested state version",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					strings.Replace(string(validBefore), `"version": 8`, `"version": 1`, 1),
				)
			},
			want: "statefile_before: unsupported statefile version 1",
		},
		{
			name: "empty retired nested state version",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					`{"version":7,"managed_paths":[],"managed_aggregate_contributions":[],"pending_carrier_installs":[],"pending_carrier_removals":[],"managed_carrier_claims":[],"delegate_attempts":[],"host_route_attempts":[]}`,
				)
			},
			want: "statefile_before: unsupported statefile version 7",
		},
		{
			name: "missing nested state families",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(`{"version":8}`)
			},
			want: "requires every durable fact-family array",
		},
		{
			name: "duplicate nested state key",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					strings.Replace(string(validBefore), `"version": 8`, `"version": 8, "version": 8`, 1),
				)
			},
			want: "duplicate object key",
		},
		{
			name: "null nested state",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(`null`)
			},
			want: "statefile must be a JSON object",
		},
		{
			name: "unknown nested state field",
			mutate: func(candidate *recoveryJournalDTO) {
				candidate.StatefileBefore = json.RawMessage(
					strings.Replace(string(validBefore), `"version": 8`, `"version": 8, "ready": true`, 1),
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

func TestLoadRecoveryJournalRequiresTopLevelEntriesPresence(t *testing.T) {
	journal := defaultRecoveryJournal()
	journal.Entries = nil
	journal.ProjectRootProvenance = nil
	content, err := marshalRecoveryJournal(journal, testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		entries json.RawMessage
		omit    bool
		wantErr string
	}{
		{
			name:    "missing",
			omit:    true,
			wantErr: `recovery journal field "entries" is required`,
		},
		{
			name:    "explicit null",
			entries: json.RawMessage(`null`),
		},
		{
			name:    "explicit empty array",
			entries: json.RawMessage(`[]`),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := make(map[string]json.RawMessage, len(document))
			for name, value := range document {
				candidate[name] = append(json.RawMessage(nil), value...)
			}
			if test.omit {
				delete(candidate, "entries")
			} else {
				candidate["entries"] = test.entries
			}
			mutated := mustMarshalRecoveryJSON(t, candidate)

			directory, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, recoveryJournalFileName)
			if err := os.WriteFile(path, mutated, recoveryJournalMode); err != nil {
				t.Fatal(err)
			}
			_, err = loadRecoveryJournal(
				t.Context(),
				journalTestFilesystem(),
				path,
				testStateCodec(),
			)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("loadRecoveryJournal: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("loadRecoveryJournal error = %v, want %q", err, test.wantErr)
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
				`"version": 10`,
				`"version": 10, "version": 10`,
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

func TestLoadRecoveryJournalRejectsNoncanonicalProvisionalIntentKeys(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	content = bytes.TrimSpace(content)
	if len(content) == 0 || content[len(content)-1] != '}' {
		t.Fatalf("recovery journal = %q, want object", content)
	}
	content = append(
		append([]byte(nil), content[:len(content)-1]...),
		[]byte(`,"provisional_acquire_intents":[{"Kind":"provisional_acquire"}]}`)...,
	)

	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, recoveryJournalFileName)
	if err := os.WriteFile(path, content, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, testStateCodec()); err == nil ||
		!strings.Contains(err.Error(), "must use canonical ASCII lower_snake_case spelling") {
		t.Fatalf("loadRecoveryJournal error = %v, want noncanonical-key rejection", err)
	}
}

func TestLoadRecoveryJournalRequiresEntryFieldPresence(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	fields := []struct {
		name   string
		parent string
		child  string
	}{
		{name: "subject", parent: "subject"},
		{name: "scope", parent: "scope"},
		{name: "path", parent: "path"},
		{name: "before", parent: "before"},
		{name: "expected_after", parent: "expected_after"},
		{name: "state_before", parent: "state_before"},
		{name: "state_expected_after", parent: "state_expected_after"},
		{name: "before.existed", parent: "before", child: "existed"},
		{name: "expected_after.existed", parent: "expected_after", child: "existed"},
		{name: "state_before.managed", parent: "state_before", child: "managed"},
		{
			name:   "state_expected_after.managed",
			parent: "state_expected_after",
			child:  "managed",
		},
	}
	for _, field := range fields {
		for _, state := range []struct {
			name   string
			asNull bool
			want   string
		}{
			{name: "missing", want: "is required"},
			{name: "null", asNull: true, want: "must not be null"},
		} {
			t.Run(field.name+"/"+state.name, func(t *testing.T) {
				mutated := mutateRecoveryEntryField(
					t,
					content,
					field.parent,
					field.child,
					state.asNull,
				)
				loadErr, planErr := malformedRecoveryJournalErrors(t, mutated)
				fieldName := field.parent
				if field.child != "" {
					fieldName = field.child
				}
				for label, err := range map[string]error{
					"load": loadErr,
					"plan": planErr,
				} {
					if err == nil ||
						!strings.Contains(err.Error(), `field "`+fieldName+`"`) ||
						!strings.Contains(err.Error(), state.want) {
						t.Fatalf(
							"%s error = %v, want field %q %s",
							label,
							err,
							fieldName,
							state.want,
						)
					}
				}
			})
		}
	}
}

func TestRecoveryJournalRequiredZeroFieldsRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		content   []byte
		roundTrip func([]byte) ([]byte, error)
	}{
		{
			name:    "before absent",
			content: []byte(`{"existed":false}`),
			roundTrip: func(content []byte) ([]byte, error) {
				var value recoveryBeforePathDTO
				if err := json.Unmarshal(content, &value); err != nil {
					return nil, err
				}
				return json.Marshal(value)
			},
		},
		{
			name:    "expected absent",
			content: []byte(`{"existed":false}`),
			roundTrip: func(content []byte) ([]byte, error) {
				var value recoveryExpectedPathDTO
				if err := json.Unmarshal(content, &value); err != nil {
					return nil, err
				}
				return json.Marshal(value)
			},
		},
		{
			name:    "unmanaged membership",
			content: []byte(`{"managed":false}`),
			roundTrip: func(content []byte) ([]byte, error) {
				var value recoveryManagedMembership
				if err := json.Unmarshal(content, &value); err != nil {
					return nil, err
				}
				return json.Marshal(value)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roundTrip, err := test.roundTrip(test.content)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(roundTrip, test.content) {
				t.Fatalf("round trip = %s, want %s", roundTrip, test.content)
			}
		})
	}
}

func TestRecoveryJournalPresenceDecoderPreservesNestedUnknownFieldRejection(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(document["entries"], &entries); err != nil {
		t.Fatal(err)
	}
	var before map[string]json.RawMessage
	if err := json.Unmarshal(entries[0]["before"], &before); err != nil {
		t.Fatal(err)
	}
	before["unexpected"] = json.RawMessage(`true`)
	entries[0]["before"], err = json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	document["entries"], err = json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}

	loadErr, _ := malformedRecoveryJournalErrors(t, mutated)
	if loadErr == nil || !strings.Contains(loadErr.Error(), `unknown field "unexpected"`) {
		t.Fatalf("load error = %v, want nested unknown-field rejection", loadErr)
	}
}

func mutateRecoveryEntryField(
	t *testing.T,
	content []byte,
	parent string,
	child string,
	asNull bool,
) []byte {
	t.Helper()
	var document map[string]json.RawMessage
	if err := json.Unmarshal(content, &document); err != nil {
		t.Fatal(err)
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(document["entries"], &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}
	target := entries[0]
	field := parent
	if child != "" {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(target[parent], &nested); err != nil {
			t.Fatal(err)
		}
		target = nested
		field = child
	}
	if asNull {
		target[field] = json.RawMessage(`null`)
	} else {
		delete(target, field)
	}
	if child != "" {
		entries[0][parent] = mustMarshalRecoveryJSON(t, target)
	}
	document["entries"] = mustMarshalRecoveryJSON(t, entries)
	return mustMarshalRecoveryJSON(t, document)
}

func mustMarshalRecoveryJSON(t *testing.T, value any) []byte {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func malformedRecoveryJournalErrors(
	t *testing.T,
	content []byte,
) (error, error) {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	recoveryRoot := filepath.Join(directory, "recovery")
	operationDir := filepath.Join(recoveryRoot, testOperationID)
	if err := os.MkdirAll(operationDir, 0o700); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(operationDir, recoveryJournalFileName)
	if err := os.WriteFile(journalPath, content, recoveryJournalMode); err != nil {
		t.Fatal(err)
	}
	_, loadErr := loadRecoveryJournal(
		t.Context(),
		journalTestFilesystem(),
		journalPath,
		testStateCodec(),
	)
	plan, planErr := LoadRecoverablePlanWithOptions(
		t.Context(),
		Paths{RecoveryDir: recoveryRoot},
		PlanLoadOptions{
			Filesystem: journalTestFilesystem(),
			StateCodec: testStateCodec(),
		},
	)
	if plan != nil {
		t.Fatalf("malformed recovery journal produced plan %#v", plan)
	}
	return loadErr, planErr
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
