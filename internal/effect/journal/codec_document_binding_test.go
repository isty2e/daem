package journal

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestDocumentBindingIntentWirePreservesAllCanonicalVariants(t *testing.T) {
	root := t.TempDir()
	statefile, err := pathauthority.NewExact(filepath.Join(root, "state.json"), "exact-v1:")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(statefile, filepath.Join(root, "original.toml"))
	if err != nil {
		t.Fatal(err)
	}
	alias, err := stateauthority.New(statefile, filepath.Join(root, "alias.toml"))
	if err != nil {
		t.Fatal(err)
	}
	exact := pathtest.DarwinCaseSensitive(filepath.Join(root, "host", "Caf\u00e9.json"))
	namespace := pathtest.DarwinCaseSensitive(root)
	provisional, err := pathauthority.NewProvisional(exact.Key(), exact.Witness(), namespace.Key(), namespace.Witness())
	if err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{".pi/settings.json", "@pi-agent/settings.json"} {
		t.Run(spelling, func(t *testing.T) {
			destination, err := output.Parse(spelling)
			if err != nil {
				t.Fatal(err)
			}
			binding, err := ownership.NewDocumentBinding(owner, destination, exact)
			if err != nil {
				t.Fatal(err)
			}
			present, err := ownership.PresentDocumentBinding(binding)
			if err != nil {
				t.Fatal(err)
			}
			for _, phases := range [][2]ownership.DocumentBindingValue{
				{ownership.NoDocumentBinding(), present}, {present, present}, {present, ownership.NoDocumentBinding()},
			} {
				intent, err := ownership.NewExactDocumentBindingIntent(alias, destination, phases[0], phases[1])
				if err != nil {
					t.Fatal(err)
				}
				if !intent.Owner().ExactEqual(owner) {
					t.Fatal("fixture did not canonicalize to stored provenance")
				}
				assertBindingIntentWireRoundTrip(t, intent)
			}
			pending, err := ownership.NewProvisionalDocumentBindingIntent(alias, destination, provisional)
			if err != nil {
				t.Fatal(err)
			}
			assertBindingIntentWireRoundTrip(t, pending)
			promoted, err := pending.Promote(exact)
			if err != nil {
				t.Fatal(err)
			}
			assertBindingIntentWireRoundTrip(t, promoted)
		})
	}
}

func TestDocumentBindingIntentWireRefusesMalformedOrContradictoryEvidence(t *testing.T) {
	root := t.TempDir()
	base := bindingWireTestRecord(t, root)
	for _, test := range []struct {
		name string
		edit func(*recoveryDocumentBindingIntent)
	}{
		{name: "empty owner", edit: func(r *recoveryDocumentBindingIntent) { r.StatefileAuthority = pathAuthorityDTO{} }},
		{name: "invalid provenance", edit: func(r *recoveryDocumentBindingIntent) { r.ManifestPath = "relative.toml" }},
		{name: "empty destination", edit: func(r *recoveryDocumentBindingIntent) { r.Destination = "" }},
		{name: "absent absence", edit: func(r *recoveryDocumentBindingIntent) { r.After = recoveryDocumentBindingPhase{Kind: "absent"} }},
		{name: "unknown phase", edit: func(r *recoveryDocumentBindingIntent) { r.After.Kind = "reserved" }},
		{name: "missing exact", edit: func(r *recoveryDocumentBindingIntent) { r.After.Authority = nil }},
		{name: "absent payload", edit: func(r *recoveryDocumentBindingIntent) { r.Before.Authority = r.After.Authority }},
		{name: "exact namespace", edit: func(r *recoveryDocumentBindingIntent) { r.After.NamespaceAuthority = r.After.Authority }},
		{name: "provisional before", edit: func(r *recoveryDocumentBindingIntent) { r.Before.Kind = "provisional" }},
		{name: "missing provisional namespace", edit: func(r *recoveryDocumentBindingIntent) { r.After.Kind = "provisional" }},
		{name: "missing provisional candidate", edit: func(r *recoveryDocumentBindingIntent) {
			r.After.Kind, r.After.NamespaceAuthority, r.After.Authority = "provisional", r.After.Authority, nil
		}},
		{name: "outside provisional namespace", edit: func(r *recoveryDocumentBindingIntent) {
			r.After.Kind = "provisional"
			r.After.NamespaceAuthority = persistedPathAuthority(pathtest.DarwinCaseSensitive(filepath.Join(root, "different")))
		}},
		{name: "retarget exact", edit: func(r *recoveryDocumentBindingIntent) {
			r.Before = r.After
			r.After.Authority = &pathAuthorityDTO{Key: filepath.Join(root, "different.json"), Witness: "exact-v1:"}
		}},
		{name: "provisional with before", edit: func(r *recoveryDocumentBindingIntent) {
			r.Before = r.After
			r.After.Kind = "provisional"
			r.After.NamespaceAuthority = persistedPathAuthority(pathtest.DarwinCaseSensitive(root))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := base
			test.edit(&record)
			content, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := decodeBindingIntentWireForTest(content); err == nil || result.Validate() == nil {
				t.Fatal("invalid wire evidence produced a usable canonical intent")
			}
		})
	}
	if _, err := persistedDocumentBindingIntent(ownership.DocumentBindingIntent{}); err == nil {
		t.Fatal("zero intent was serialized")
	}
}

func TestDocumentBindingIntentWireRequiresExactJSONStructure(t *testing.T) {
	encoded, err := json.Marshal(bindingWireTestRecord(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"statefile_authority", "manifest_path", "destination", "before", "after"} {
		for _, null := range []bool{false, true} {
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &fields); err != nil {
				t.Fatal(err)
			}
			if null {
				fields[field] = json.RawMessage("null")
			} else {
				delete(fields, field)
			}
			content, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeBindingIntentWireForTest(content); err == nil {
				t.Fatalf("missing/null required %s was admitted", field)
			}
		}
	}
	for _, replacement := range []string{
		`"before":{}`, `"before":{"kind":null}`, `"before":{"kind":"absent","extra":1}`,
		`"before":{"Kind":"absent"}`, `"before":{"kind":"absent","kind":"absent"}`,
		`"before":{"kind":"absent","Kind":"absent"}`, `"before":{"kind":"absent","Authority":null}`,
		`"before":[]`, `"before":{"kind":"absent"},"before":{"kind":"absent"}`,
	} {
		content := strings.Replace(string(encoded), `"before":{"kind":"absent"}`, replacement, 1)
		if content == string(encoded) {
			t.Fatal("fixture substitution did not create malformed structure")
		}
		if _, err := decodeBindingIntentWireForTest([]byte(content)); err == nil {
			t.Fatalf("invalid structure was admitted: %s", replacement)
		}
	}
}

func TestLegacyJournalRefusesDocumentBindingIntentPayload(t *testing.T) {
	content, err := marshalRecoveryJournal(defaultRecoveryJournal(), testStateCodec())
	if err != nil {
		t.Fatal(err)
	}
	var original recoveryJournalDTO
	if err := json.Unmarshal(content, &original); err != nil {
		t.Fatalf("existing journal no longer decodes: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal([]recoveryDocumentBindingIntent{bindingWireTestRecord(t, t.TempDir())})
	if err != nil {
		t.Fatal(err)
	}
	fields["document_binding_intents"] = payload
	content, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var decoded recoveryJournalDTO
	if err := json.Unmarshal(content, &decoded); err == nil {
		t.Fatal("legacy journal silently admitted an unintegrated binding family")
	}
}

func assertBindingIntentWireRoundTrip(t *testing.T, intent ownership.DocumentBindingIntent) {
	t.Helper()
	record, err := persistedDocumentBindingIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeBindingIntentWireForTest(content)
	if err != nil || !decoded.Equal(intent) {
		t.Fatalf("wire lost exact/provisional phase or stored provenance: %v", err)
	}
}

func decodeBindingIntentWireForTest(content []byte) (ownership.DocumentBindingIntent, error) {
	if err := jsonstrict.Validate(content, "test recovery document binding", maximumRecoveryJournalJSONDepth); err != nil {
		return ownership.DocumentBindingIntent{}, err
	}
	var record recoveryDocumentBindingIntent
	if err := json.Unmarshal(content, &record); err != nil {
		return ownership.DocumentBindingIntent{}, err
	}
	return record.canonical()
}

func bindingWireTestRecord(t *testing.T, root string) recoveryDocumentBindingIntent {
	t.Helper()
	return recoveryDocumentBindingIntent{
		StatefileAuthority: pathAuthorityDTO{Key: filepath.Join(root, "state.json"), Witness: "exact-v1:"},
		ManifestPath:       filepath.Join(root, "daem.toml"), Destination: "@pi-agent/settings.json",
		Before: recoveryDocumentBindingPhase{Kind: "absent"},
		After:  recoveryDocumentBindingPhase{Kind: "exact", Authority: persistedPathAuthority(pathtest.DarwinCaseSensitive(filepath.Join(root, "Caf\u00e9.json")))},
	}
}
