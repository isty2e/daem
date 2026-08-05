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

func TestScopeFlagValuesNormalizeCanonicalScopesAtIngress(t *testing.T) {
	var values scopeFlagValues
	for _, value := range []string{"project", "project", "global", "global"} {
		if err := values.Set(value); err != nil {
			t.Fatalf("Set(%q) returned error: %v", value, err)
		}
	}

	want := []string{"project", "global"}
	if got := values.strings(); !slices.Equal(got, want) {
		t.Fatalf("strings = %#v, want %#v", got, want)
	}
	if got := values.String(); got != "[project global]" {
		t.Fatalf("String = %q", got)
	}

	for _, invalid := range []string{
		"",
		" ",
		" project",
		"project ",
		"project,global",
		"PROJECT",
		"Global",
		"unknown",
		"project/global",
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

func TestSingleScopeValuePreservesCommandCardinalityPolicy(t *testing.T) {
	for _, test := range []struct {
		name    string
		values  scopeFlagValues
		want    string
		wantErr string
	}{
		{name: "omitted"},
		{name: "project", values: scopeFlagValues{target.ScopeProject}, want: "project"},
		{name: "global", values: scopeFlagValues{target.ScopeGlobal}, want: "global"},
		{
			name:    "project and global",
			values:  scopeFlagValues{target.ScopeProject, target.ScopeGlobal},
			wantErr: "--scope accepts at most one distinct scope for this command",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := singleScopeValue(test.values)
			if test.wantErr != "" {
				if err == nil || err.Error() != test.wantErr {
					t.Fatalf("error = %v, want %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("singleScopeValue returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("singleScopeValue = %q, want %q", got, test.want)
			}
		})
	}
}
