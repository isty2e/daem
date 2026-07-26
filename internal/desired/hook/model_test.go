package hook

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

func TestHookConstructorOwnsOverridesAndDefensiveStorage(t *testing.T) {
	targets := []target.Target{target.TargetCodex, target.TargetClaudeCode}
	overrides := map[target.Target]TargetOverride{
		target.TargetClaudeCode: NewTargetOverride("tool", "Write"),
	}
	hook, err := New(Spec{
		Name:            " protect ",
		Event:           " Stop ",
		Type:            TypeCommand,
		Command:         " ./protect.sh ",
		Targets:         targets,
		Scope:           target.ScopeProject,
		TargetOverrides: overrides,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets[0] = target.TargetPi
	overrides[target.TargetClaudeCode] = NewTargetOverride("changed", "changed")
	if hook.ID().Kind() != entity.KindHook || hook.ID().Name() != "protect" || hook.Event() != "Stop" || hook.Command() != "./protect.sh" {
		t.Fatalf("hook normalization mismatch")
	}
	got := hook.TargetOverrides()
	if got[target.TargetClaudeCode].Matcher() != "Write" {
		t.Fatalf("override = %#v", got)
	}
	got[target.TargetClaudeCode] = NewTargetOverride("mutated", "mutated")
	if hook.TargetOverrides()[target.TargetClaudeCode].Matcher() != "Write" {
		t.Fatal("TargetOverrides returned aliased storage")
	}
	if hook.Targets()[0] != target.TargetCodex || hook.Validate() != nil {
		t.Fatalf("targets or validation mismatch")
	}
}

func TestHookOwnsCanonicalAssetReferences(t *testing.T) {
	hook, err := New(Spec{
		Name:    "protect",
		Event:   "Stop",
		Type:    TypeCommand,
		Command: "run {hook_file:guard} {hook_file:audit} {hook_file:guard}",
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	})
	if err != nil {
		t.Fatal(err)
	}
	references := hook.AssetReferences()
	if len(references) != 2 ||
		references[0].ID() != "audit" ||
		references[1].ID() != "guard" {
		t.Fatalf("AssetReferences = %#v, want canonical audit/guard references", references)
	}
	references[0] = AssetReference{id: "changed"}
	if slices.Equal(references, hook.AssetReferences()) {
		t.Fatal("AssetReferences returned aliased storage")
	}

	forged := hook
	forged.assetReferences = references
	if err := forged.Validate(); err == nil || !strings.Contains(err.Error(), "do not match command") {
		t.Fatalf("forged Hook Validate error = %v", err)
	}
}

func TestHookConstructorRejectsInvalidAxes(t *testing.T) {
	base := Spec{
		Name:    "protect",
		Event:   "Stop",
		Type:    TypeCommand,
		Command: "./protect.sh",
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	}
	tests := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{name: "empty name", edit: func(spec *Spec) { spec.Name = " " }, want: "name is required"},
		{name: "empty event", edit: func(spec *Spec) { spec.Event = " " }, want: "event is required"},
		{name: "control event", edit: func(spec *Spec) { spec.Event = "Stop\u0085hidden" }, want: "control characters"},
		{name: "unknown type", edit: func(spec *Spec) { spec.Type = "http" }, want: "unknown hook type"},
		{name: "empty command", edit: func(spec *Spec) { spec.Command = " " }, want: "command is required"},
		{name: "malformed placeholder", edit: func(spec *Spec) {
			spec.Command = "run {hook_file:guard"
		}, want: "missing closing brace"},
		{name: "unsupported placeholder kind", edit: func(spec *Spec) {
			spec.Command = "run {hook_dir:guard}"
		}, want: "hook_dir placeholders are unsupported"},
		{name: "path-like placeholder", edit: func(spec *Spec) {
			spec.Command = "run {hook_file:../guard}"
		}, want: "path-like"},
		{name: "negative timeout", edit: func(spec *Spec) { spec.TimeoutSeconds = -1 }, want: "must not be negative"},
		{name: "missing targets", edit: func(spec *Spec) { spec.Targets = nil }, want: "at least one target"},
		{name: "unknown scope", edit: func(spec *Spec) { spec.Scope = "workspace" }, want: "unknown scope"},
		{name: "override outside targets", edit: func(spec *Spec) {
			spec.TargetOverrides = map[target.Target]TargetOverride{target.TargetClaudeCode: {}}
		}, want: "is not declared"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec := base
			test.edit(&spec)
			if _, err := New(spec); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New error = %v, want containing %q", err, test.want)
			}
		})
	}
	if err := (Hook{}).Validate(); err == nil {
		t.Fatal("zero Hook validated")
	}
}

func TestHookConstructorRejectsControlCharactersInIdentity(t *testing.T) {
	for _, name := range []string{"protect\x00hidden", "protect\nhidden", "protect\u0085hidden", "safe\u202etxt"} {
		_, err := New(Spec{
			Name: name, Event: "Stop", Type: TypeCommand, Command: "echo ok",
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		})
		if err == nil {
			t.Fatalf("New accepted control-bearing hook name %q", name)
		}
	}
}

func TestHookConstructorRejectsInvalidUTF8Text(t *testing.T) {
	invalid := string([]byte{'b', 'a', 'd', 0xff})
	base := Spec{
		Name: "protect", Event: "Stop", Type: TypeCommand, Command: "echo ok",
		Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
	}
	for _, edit := range []func(*Spec){
		func(spec *Spec) { spec.Event = invalid },
		func(spec *Spec) { spec.Matcher = invalid },
		func(spec *Spec) { spec.Command = invalid },
		func(spec *Spec) { spec.StatusMessage = invalid },
		func(spec *Spec) {
			spec.TargetOverrides = map[target.Target]TargetOverride{
				target.TargetCodex: NewTargetOverride(invalid, ""),
			}
		},
	} {
		spec := base
		edit(&spec)
		if _, err := New(spec); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
			t.Fatalf("New error = %v, want invalid UTF-8 rejection", err)
		}
	}
}
