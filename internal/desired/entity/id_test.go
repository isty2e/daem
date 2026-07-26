package entity

import (
	"slices"
	"strings"
	"testing"
)

func TestIDRoundTripsEveryCurrentFamily(t *testing.T) {
	kinds := []Kind{
		KindSkill,
		KindHook,
		KindHookAsset,
		KindInstructions,
		KindMCPServer,
		KindExtension,
	}

	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			id, err := New(kind, "review:team/alpha beta")
			if err != nil {
				t.Fatalf("New returned error: %v", err)
			}
			parsed, err := Parse(id.String())
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", id.String(), err)
			}
			if parsed != id {
				t.Fatalf("Parse(%q) = %#v, want %#v", id.String(), parsed, id)
			}
		})
	}
}

func TestIDIdentityIgnoresNonIdentityAxes(t *testing.T) {
	first, err := New(KindSkill, "review")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	second, err := New(KindSkill, "review")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if first != second {
		t.Fatalf("same authored family/name produced different IDs: %#v != %#v", first, second)
	}

	renamed, err := New(KindSkill, "review-v2")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if renamed == first {
		t.Fatal("renaming an authored entity did not change identity")
	}
}

func TestIDPreservesFamilyNormalizedNameExactly(t *testing.T) {
	id, err := New(KindInstructions, " project instructions ")
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if id.Name() != " project instructions " {
		t.Fatalf("Name = %q, want exact family-normalized value", id.Name())
	}
	parsed, err := Parse(id.String())
	if err != nil {
		t.Fatalf("Parse(%q) returned error: %v", id.String(), err)
	}
	if parsed != id {
		t.Fatalf("round trip = %#v, want %#v", parsed, id)
	}
}

func TestIDCompareIsStable(t *testing.T) {
	values := []ID{
		mustID(t, KindSkill, "zeta"),
		mustID(t, KindHook, "alpha"),
		mustID(t, KindSkill, "alpha"),
	}
	slices.SortFunc(values, Compare)

	got := []string{values[0].String(), values[1].String(), values[2].String()}
	want := []string{"hook:alpha", "skill:alpha", "skill:zeta"}
	if !slices.Equal(got, want) {
		t.Fatalf("ordered IDs = %#v, want %#v", got, want)
	}
}

func TestIDRejectsMalformedAndNonCanonicalText(t *testing.T) {
	for _, value := range []string{
		"",
		"skill",
		"unknown:name",
		"skill:",
		"skill:%61lpha",
		"skill:%zz",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := Parse(value); err == nil {
				t.Fatalf("Parse(%q) returned nil error", value)
			}
		})
	}

	if _, err := New(KindSkill, " \t\n"); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("New whitespace-only name error = %v", err)
	}
	if _, err := New(Kind("future"), "name"); err == nil || !strings.Contains(err.Error(), "unknown desired entity kind") {
		t.Fatalf("New unknown kind error = %v", err)
	}
}

func TestIDRejectsInvalidUTF8Identity(t *testing.T) {
	name := string([]byte{'r', 'e', 'v', 'i', 'e', 'w', 0xff})
	if _, err := New(KindSkill, name); err == nil {
		t.Fatal("New accepted an identity that cannot round-trip through UTF-8 serialization")
	}
}

func TestZeroIDHasNoTextualIdentity(t *testing.T) {
	var zero ID
	if zero.String() != "" || zero.Kind() != "" || zero.Name() != "" {
		t.Fatalf("zero ID = kind %q name %q text %q", zero.Kind(), zero.Name(), zero.String())
	}
	if err := zero.Validate(); err == nil {
		t.Fatal("zero ID validated successfully")
	}

	valid := mustID(t, KindSkill, "review")
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid ID failed validation: %v", err)
	}
}

func mustID(t *testing.T, kind Kind, name string) ID {
	t.Helper()
	id, err := New(kind, name)
	if err != nil {
		t.Fatalf("New(%q, %q) returned error: %v", kind, name, err)
	}
	return id
}
