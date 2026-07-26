package output

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestDestinationValidationRejectsNonCanonicalAndScopeContradictoryValues(t *testing.T) {
	t.Parallel()

	for name, destination := range map[string]Destination{
		"empty":             "",
		"parent escape":     "../escape",
		"absolute":          Destination(filepath.Join(string(filepath.Separator), "tmp", "escape")),
		"backslash":         `nested\escape`,
		"redundant segment": "nested/./escape",
		"unknown role":      "@cache/escape",
		"control":           "nested/\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := destination.Validate(); err == nil {
				t.Fatalf("Destination(%q).Validate returned nil error", destination)
			}
		})
	}

	for _, test := range []struct {
		name        string
		destination Destination
		scope       target.Scope
	}{
		{name: "project uses home", destination: "~/agents/skills", scope: target.ScopeProject},
		{name: "project uses data", destination: "@data/skills", scope: target.ScopeProject},
		{name: "global uses project", destination: ".agents/skills", scope: target.ScopeGlobal},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.destination.ValidateScope(test.scope); err == nil {
				t.Fatalf("Destination(%q).ValidateScope(%q) returned nil error", test.destination, test.scope)
			}
		})
	}

	for _, test := range []struct {
		destination Destination
		scope       target.Scope
	}{
		{destination: ".agents/skills", scope: target.ScopeProject},
		{destination: "~/agents/skills", scope: target.ScopeGlobal},
		{destination: "@data/skills", scope: target.ScopeGlobal},
	} {
		if err := test.destination.ValidateScope(test.scope); err != nil {
			t.Fatalf("Destination(%q).ValidateScope(%q) returned error: %v", test.destination, test.scope, err)
		}
	}
}

func TestPortableRoundTripsEveryRootRole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value    string
		root     RootRole
		relative string
		scope    target.Scope
	}{
		{value: ".agents/skills/review", root: RootProject, relative: ".agents/skills/review", scope: target.ScopeProject},
		{value: "~/.agents/skills/review", root: RootHome, relative: ".agents/skills/review", scope: target.ScopeGlobal},
		{value: "@data/hook-assets/guard/sha256-deadbeef/asset", root: RootData, relative: "hook-assets/guard/sha256-deadbeef/asset", scope: target.ScopeGlobal},
	}
	for _, test := range cases {
		t.Run(string(test.root), func(t *testing.T) {
			destination, err := Parse(test.value)
			if err != nil {
				t.Fatal(err)
			}
			if destination.RootRole() != test.root || destination.RelativePath() != test.relative || destination.String() != test.value {
				t.Fatalf("destination = root %q relative %q string %q", destination.RootRole(), destination.RelativePath(), destination.String())
			}
			if err := destination.ValidateScope(test.scope); err != nil {
				t.Fatalf("ValidateScope(%q): %v", test.scope, err)
			}
			reparsed, err := Parse(destination.String())
			if err != nil || reparsed != destination {
				t.Fatalf("reparse = %#v, %v; want %#v", reparsed, err, destination)
			}
		})
	}
}

func TestPortableRejectsMalformedAndEscapingSpellings(t *testing.T) {
	t.Parallel()

	cases := []string{
		"", ".", "..", "../escape", "a/../b", "a//b", "a/", "/absolute", "//server/share", `C:\\absolute`, "C:/absolute", "c:relative",
		`a\\b`, "~", "~/", "~/../escape", "~other/path", "@data", "@data/", "@data/../escape",
		"@datax/path", "@Data/path", "@cache/path", " spaced", "spaced ", "nul\x00byte", "line\nbreak", "\u202eright-to-left", string([]byte{0xff}),
	}
	for _, value := range cases {
		t.Run(strings.ReplaceAll(value, "/", "_"), func(t *testing.T) {
			if _, err := Parse(value); err == nil {
				t.Fatalf("Parse(%q) returned nil error", value)
			}
		})
	}
}

func TestPortableTreatsReservedMarkersAsOrdinaryNonLeadingComponents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value string
		root  RootRole
	}{
		{value: "folder/@data/asset", root: RootProject},
		{value: "~/.agents/@data/skill", root: RootHome},
		{value: "@data/~archive/asset", root: RootData},
		{value: "literal/%2e%2e/asset", root: RootProject},
	}
	for _, test := range cases {
		destination, err := Parse(test.value)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.value, err)
		}
		if destination.RootRole() != test.root || destination.String() != test.value {
			t.Fatalf("Parse(%q) = (%q, %q)", test.value, destination.RootRole(), destination.String())
		}
	}
}

func TestPortableZeroValueIsNeverValid(t *testing.T) {
	t.Parallel()

	var destination Portable
	if destination.String() != "" {
		t.Fatalf("zero destination string = %q", destination.String())
	}
	if err := destination.ValidateScope(target.ScopeProject); err == nil {
		t.Fatal("zero destination passed project scope validation")
	}
}

func TestPortableRejectsScopeContradictions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value string
		scope target.Scope
	}{
		{value: "project/path", scope: target.ScopeGlobal},
		{value: "~/global/path", scope: target.ScopeProject},
		{value: "@data/global/path", scope: target.ScopeProject},
	}
	for _, test := range cases {
		destination, err := Parse(test.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := destination.ValidateScope(test.scope); err == nil {
			t.Fatalf("destination %q admitted contradictory scope %q", test.value, test.scope)
		}
	}
}

func TestPortableConstructorRejectsAmbiguousProjectSpelling(t *testing.T) {
	t.Parallel()

	for _, relative := range []string{"@data/asset", "@future/asset", "~/asset"} {
		if _, err := newPortable(RootProject, relative); err == nil {
			t.Fatalf("newPortable(project, %q) returned nil error", relative)
		}
	}
	if _, err := newPortable(RootRole("cache"), "asset"); err == nil {
		t.Fatal("newPortable admitted an unknown root role")
	}
}

func TestPortableScopeValidationCoversEveryRootAndScopeCombination(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value string
		scope target.Scope
		valid bool
	}{
		{value: "project/file", scope: target.ScopeProject, valid: true},
		{value: "project/file", scope: target.ScopeGlobal},
		{value: "~/global/file", scope: target.ScopeProject},
		{value: "~/global/file", scope: target.ScopeGlobal, valid: true},
		{value: "@data/global/file", scope: target.ScopeProject},
		{value: "@data/global/file", scope: target.ScopeGlobal, valid: true},
		{value: "project/file", scope: target.Scope("workspace")},
		{value: "~/global/file", scope: target.Scope("workspace")},
		{value: "@data/global/file", scope: target.Scope("workspace")},
	}
	for _, test := range cases {
		destination, err := Parse(test.value)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.value, err)
		}
		err = destination.ValidateScope(test.scope)
		if test.valid && err != nil {
			t.Errorf("destination %q rejected scope %q: %v", test.value, test.scope, err)
		}
		if !test.valid && err == nil {
			t.Errorf("destination %q admitted scope %q", test.value, test.scope)
		}
	}
}

func TestPortablePreservesValidUnicodePathComponents(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"instructions/프로젝트.md",
		"~/.agents/skills/検証/SKILL.md",
		"@data/hooks/실행/asset",
	} {
		destination, err := Parse(value)
		if err != nil {
			t.Fatalf("Parse(%q): %v", value, err)
		}
		if destination.String() != value {
			t.Fatalf("Parse(%q).String() = %q", value, destination.String())
		}
	}
}
