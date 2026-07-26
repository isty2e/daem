package instructions

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestInstructionsConstructorOwnsRenderingAndDefensiveStorage(t *testing.T) {
	rendering, err := NewRendering(" ../host/decides ", RenderModeCopy)
	if err != nil {
		t.Fatalf("NewRendering returned error: %v", err)
	}
	targets := []target.Target{target.TargetCodex, target.TargetClaudeCode}
	renderings := map[target.Target]Rendering{target.TargetCodex: rendering}
	value, err := New(Spec{
		Name:       "project",
		Source:     sourcetest.Local(t, "AGENTS.md", source.LocalSourceModeVendor),
		Targets:    targets,
		Scope:      target.ScopeProject,
		Renderings: renderings,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	targets[0] = target.TargetPi
	changed, _ := NewRendering("changed", RenderModeSymlink)
	renderings[target.TargetCodex] = changed
	if value.ID().Kind() != entity.KindInstructions || value.ID().Name() != "project" {
		t.Fatalf("ID = %q", value.ID())
	}
	got := value.Renderings()
	if got[target.TargetCodex].RenderTo() != "../host/decides" || got[target.TargetCodex].Mode() != RenderModeCopy {
		t.Fatalf("renderings = %#v", got)
	}
	got[target.TargetCodex] = changed
	if value.Renderings()[target.TargetCodex].Mode() != RenderModeCopy {
		t.Fatal("Renderings returned aliased storage")
	}
	if value.Targets()[0] != target.TargetCodex || value.Validate() != nil {
		t.Fatal("canonical instructions did not retain owned state")
	}
}

func TestInstructionsConstructorRejectsInvalidAxesAndZero(t *testing.T) {
	copyRendering, _ := NewRendering("AGENTS.md", RenderModeCopy)
	base := Spec{
		Name:       "project",
		Source:     sourcetest.Local(t, "AGENTS.md", source.LocalSourceModeVendor),
		Targets:    []target.Target{target.TargetCodex},
		Scope:      target.ScopeProject,
		Renderings: map[target.Target]Rendering{target.TargetCodex: copyRendering},
	}
	tests := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{name: "empty name", edit: func(spec *Spec) { spec.Name = " " }, want: "name is required"},
		{name: "git source", edit: func(spec *Spec) {
			spec.Source = mustGitSource(t)
		}, want: "git instruction"},
		{name: "local link", edit: func(spec *Spec) { spec.Source = sourcetest.Local(t, "AGENTS.md", source.LocalSourceModeLink) }, want: "vendor mode"},
		{name: "archive s3", edit: func(spec *Spec) {
			spec.Source = sourcetest.S3(t, "s3://bucket/AGENTS.tar", "", "", source.S3ObjectFormatTar)
		}, want: "file format"},
		{name: "missing targets", edit: func(spec *Spec) { spec.Targets = nil }, want: "at least one target"},
		{name: "bad scope", edit: func(spec *Spec) { spec.Scope = "workspace" }, want: "unknown scope"},
		{name: "global relative", edit: func(spec *Spec) { spec.Scope = target.ScopeGlobal }, want: "absolute path"},
		{name: "rendering outside targets", edit: func(spec *Spec) {
			spec.Renderings = map[target.Target]Rendering{target.TargetClaudeCode: copyRendering}
		}, want: "not declared"},
		{name: "zero rendering", edit: func(spec *Spec) { spec.Renderings = map[target.Target]Rendering{target.TargetCodex: {}} }, want: "unknown render mode"},
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
	if _, err := NewRendering("", "append"); err == nil {
		t.Fatal("NewRendering accepted unknown mode")
	}
	if err := (Instructions{}).Validate(); err == nil {
		t.Fatal("zero Instructions validated")
	}
}

func TestInstructionsConstructorRejectsEmbeddedControlCharactersInName(t *testing.T) {
	for _, name := range []string{"project\x00hidden", "project\nhidden", "project\x7fhidden", "project\u0085hidden", "safe\u202etxt"} {
		_, err := New(Spec{
			Name: name, Source: sourcetest.Local(t, "AGENTS.md", source.LocalSourceModeVendor),
			Targets: []target.Target{target.TargetCodex}, Scope: target.ScopeProject,
		})
		if err == nil {
			t.Fatalf("New accepted control-bearing instructions name %q", name)
		}
	}
}

func TestRenderingRejectsInvalidUTF8Path(t *testing.T) {
	invalid := string([]byte{'b', 'a', 'd', 0xff})
	if _, err := NewRendering(invalid, RenderModeCopy); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("NewRendering error = %v, want invalid UTF-8 rejection", err)
	}
}

func TestRenderingRejectsBidirectionalControl(t *testing.T) {
	if _, err := NewRendering("safe\u202etxt", RenderModeCopy); err == nil || !strings.Contains(err.Error(), "bidirectional") {
		t.Fatalf("NewRendering error = %v, want bidirectional control rejection", err)
	}
}

func mustGitSource(t *testing.T) source.Source {
	t.Helper()
	value, err := source.NewGitSource("https://example.invalid/repo.git", "AGENTS.md", "main")
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return value
}
