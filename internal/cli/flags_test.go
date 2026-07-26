package cli

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestTargetFlagValuesNormalizeCanonicalTargetsAtIngress(t *testing.T) {
	var values targetFlagValues
	want := make([]string, 0, len(target.SupportedTargets()))
	for _, supported := range target.SupportedTargets() {
		value := string(supported)
		if err := values.Set(value); err != nil {
			t.Fatalf("Set(%q) returned error: %v", value, err)
		}
		if err := values.Set(value); err != nil {
			t.Fatalf("duplicate Set(%q) returned error: %v", value, err)
		}
		want = append(want, value)
	}

	if got := values.strings(); !slices.Equal(got, want) {
		t.Fatalf("strings = %#v, want %#v", got, want)
	}
	if got := values.String(); got != "[codex claude-code opencode pi antigravity-cli]" {
		t.Fatalf("String = %q", got)
	}

	for _, invalid := range []string{
		"",
		" ",
		" codex",
		"codex ",
		"codex,claude-code",
		"CODEX",
		"unknown",
		"codex/claude-code",
	} {
		before := values.strings()
		if err := values.Set(invalid); err == nil {
			t.Fatalf("Set(%q) returned nil error", invalid)
		}
		if got := values.strings(); !slices.Equal(got, before) {
			t.Fatalf("Set(%q) changed values from %#v to %#v", invalid, before, got)
		}
	}
}
