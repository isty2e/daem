package topology

import (
	"slices"
	"strings"
	"testing"
)

func TestSubjectKindsAreClosedAndRoundTrip(t *testing.T) {
	kinds := []SubjectKind{
		SubjectResource,
		SubjectProjection,
		SubjectHostRelation,
		SubjectBinding,
		SubjectCarrier,
		SubjectContribution,
		SubjectProvisionedArtifact,
		SubjectRuntimeDependency,
		SubjectCredentialReference,
	}
	seen := make(map[SubjectKind]struct{}, len(kinds))
	for _, kind := range kinds {
		if _, duplicate := seen[kind]; duplicate {
			t.Fatalf("duplicate subject kind %q", kind)
		}
		seen[kind] = struct{}{}
		parsed, err := ParseSubjectKind(string(kind))
		if err != nil || parsed != kind {
			t.Fatalf("ParseSubjectKind(%q) = %q, %v", kind, parsed, err)
		}
	}
	if _, err := ParseSubjectKind("operation"); err == nil {
		t.Fatal("ParseSubjectKind accepted an occurrence role")
	}
}

func FuzzSubjectIDKeyRoundTrip(f *testing.F) {
	for _, key := range []string{
		"simple",
		"with/slash",
		"100% literal",
		`provider:v1:{"key":"한/글"}`,
		"caf\u00e9",
		"cafe\u0301",
		"bad\x00key",
		"safe\u202etxt",
		string([]byte{'b', 'a', 'd', 0xff}),
	} {
		f.Add(key)
	}
	f.Fuzz(func(t *testing.T, key string) {
		id, err := NewSubjectID(SubjectContribution, "provider:v1", key)
		if err != nil {
			return
		}
		parsed, err := ParseSubjectID(id.String())
		if err != nil {
			t.Fatalf("ParseSubjectID(%q): %v", id.String(), err)
		}
		if parsed != id {
			t.Fatalf("round trip = %#v, want %#v", parsed, id)
		}
	})
}

func FuzzParseSubjectIDAcceptsOnlyCanonicalForm(f *testing.F) {
	for _, value := range []string{
		"projection/test/simple",
		"projection/test/with%2Fslash",
		"projection/test/key%2fpart",
		"projection/%74est/key",
		"projection/test/%zz",
		"projection/test/key/extra",
		"",
	} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		id, err := ParseSubjectID(value)
		if err != nil {
			return
		}
		if id.String() != value {
			t.Fatalf("accepted noncanonical subject %q as %q", value, id.String())
		}
		if err := id.Validate(); err != nil {
			t.Fatalf("accepted invalid subject %q: %v", value, err)
		}
	})
}

func TestSubjectIDRoundTripsDelimiterAndUnicodeKeys(t *testing.T) {
	id, err := NewSubjectID(
		SubjectContribution,
		"claude-code.plugin",
		`provider:v1:{"key":"한/글 100%"}`,
	)
	if err != nil {
		t.Fatalf("NewSubjectID returned error: %v", err)
	}
	encoded := id.String()
	if strings.Count(encoded, "/") != 2 || !strings.Contains(encoded, "%2F") {
		t.Fatalf("String() = %q, want exactly two separators and escaped key slash", encoded)
	}
	parsed, err := ParseSubjectID(encoded)
	if err != nil {
		t.Fatalf("ParseSubjectID returned error: %v", err)
	}
	if parsed != id || parsed.Kind() != SubjectContribution ||
		parsed.Namespace() != "claude-code.plugin" ||
		parsed.Key() != `provider:v1:{"key":"한/글 100%"}` {
		t.Fatalf("parsed subject = %#v", parsed)
	}
}

func TestSubjectIDSeparatesKindNamespaceAndKey(t *testing.T) {
	left := mustSubjectID(t, SubjectProjection, "codex.project.mcp-server", "shared")
	right := mustSubjectID(t, SubjectProjection, "codex.global.mcp-server", "shared")
	otherKind := mustSubjectID(t, SubjectHostRelation, "codex.project.mcp-server", "shared")
	if left == right || left == otherKind || right == otherKind {
		t.Fatal("distinct subject axes collided")
	}
}

func TestSubjectIDPreservesUnicodeCodePointIdentity(t *testing.T) {
	composed := mustSubjectID(t, SubjectContribution, "provider", "caf\u00e9")
	decomposed := mustSubjectID(t, SubjectContribution, "provider", "cafe\u0301")
	if composed == decomposed || composed.String() == decomposed.String() {
		t.Fatal("distinct Unicode code-point sequences collapsed to one subject")
	}
	for _, subject := range []SubjectID{composed, decomposed} {
		parsed, err := ParseSubjectID(subject.String())
		if err != nil || parsed != subject {
			t.Fatalf("ParseSubjectID(%q) = %#v, %v", subject.String(), parsed, err)
		}
	}
}

func TestCompareSubjectIDUsesSemanticAxes(t *testing.T) {
	values := []SubjectID{
		mustSubjectID(t, SubjectProjection, "z", "a"),
		mustSubjectID(t, SubjectHostRelation, "a", "z"),
		mustSubjectID(t, SubjectProjection, "a", "z"),
		mustSubjectID(t, SubjectProjection, "a", "a"),
	}
	slices.SortFunc(values, CompareSubjectID)
	want := []SubjectID{
		mustSubjectID(t, SubjectHostRelation, "a", "z"),
		mustSubjectID(t, SubjectProjection, "a", "a"),
		mustSubjectID(t, SubjectProjection, "a", "z"),
		mustSubjectID(t, SubjectProjection, "z", "a"),
	}
	if !slices.Equal(values, want) {
		t.Fatalf("ordered subjects = %#v, want %#v", values, want)
	}
}

func TestSubjectIDRejectsMalformedAndNoncanonicalValues(t *testing.T) {
	invalidUTF8 := string([]byte{'b', 'a', 'd', 0xff})
	constructCases := []struct {
		name      string
		kind      SubjectKind
		namespace string
		key       string
	}{
		{name: "unknown kind", kind: "attempt", namespace: "test", key: "one"},
		{name: "empty namespace", kind: SubjectBinding, key: "one"},
		{name: "path namespace", kind: SubjectBinding, namespace: "test/path", key: "one"},
		{name: "empty key", kind: SubjectBinding, namespace: "test"},
		{name: "padded key", kind: SubjectBinding, namespace: "test", key: " one "},
		{name: "invalid UTF-8", kind: SubjectBinding, namespace: "test", key: invalidUTF8},
		{name: "control key", kind: SubjectBinding, namespace: "test", key: "one\nother"},
		{name: "bidi key", kind: SubjectBinding, namespace: "test", key: "safe\u202etxt"},
	}
	for _, test := range constructCases {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSubjectID(test.kind, test.namespace, test.key); err == nil {
				t.Fatal("NewSubjectID returned nil error")
			}
		})
	}

	for _, value := range []string{
		"",
		"projection/only-two",
		"projection/too/many/parts",
		"projection/test/%zz",
		"projection/test/key%2fpart",
		"projection/%74est/key",
		"projection/test/%6bey",
		"unknown/test/key",
	} {
		if _, err := ParseSubjectID(value); err == nil {
			t.Fatalf("ParseSubjectID(%q) returned nil error", value)
		}
	}
}

func TestSubjectIDValidateRejectsZeroAndPartialValues(t *testing.T) {
	for _, id := range []SubjectID{
		{},
		{kind: SubjectProjection},
		{kind: SubjectProjection, namespace: "test"},
		{kind: SubjectProjection, namespace: "test", key: "bad\nkey"},
	} {
		if err := id.Validate(); err == nil {
			t.Fatalf("Validate accepted forged subject %#v", id)
		}
	}
	if !(SubjectID{}).IsZero() || (SubjectID{}).String() != "" {
		t.Fatal("zero subject semantics changed")
	}
}

func mustSubjectID(t *testing.T, kind SubjectKind, namespace string, key string) SubjectID {
	t.Helper()
	id, err := NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatalf("NewSubjectID(%q, %q, %q): %v", kind, namespace, key, err)
	}
	return id
}
