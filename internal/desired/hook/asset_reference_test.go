package hook

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestHookParsesCanonicalAssetReferences(t *testing.T) {
	value, err := New(Spec{
		Name:    "protect",
		Event:   "Stop",
		Type:    TypeCommand,
		Command: "python {hook_file:guard} && {hook_file:guard} && {hook_file:audit}",
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	references := value.AssetReferences()
	if len(references) != 2 ||
		references[0].ID() != "audit" ||
		references[1].ID() != "guard" {
		t.Fatalf("AssetReferences = %#v, want sorted unique audit/guard references", references)
	}
	if references[0].Placeholder() != "{hook_file:audit}" {
		t.Fatalf("Placeholder = %q", references[0].Placeholder())
	}
}

func TestHookRejectsMalformedAssetReferences(t *testing.T) {
	for _, test := range []struct {
		name    string
		command string
		wantErr string
	}{
		{name: "missing closing brace", command: "{hook_file:guard", wantErr: "missing closing brace"},
		{name: "path traversal", command: "{hook_file:../guard}", wantErr: "path-like"},
		{name: "absolute path", command: "{hook_file:/tmp/guard}", wantErr: "path-like"},
		{name: "empty id", command: "{hook_file:}", wantErr: "non-empty trimmed segment"},
		{name: "unknown namespace", command: "{hook_script:guard}", wantErr: "unknown hook asset placeholder namespace"},
		{name: "missing file colon", command: "{hook_file}", wantErr: "expected hook_file:<id> or hook_dir:<id>"},
		{name: "missing directory colon", command: "{hook_dir}", wantErr: "expected hook_file:<id> or hook_dir:<id>"},
		{name: "unsupported directory", command: "{hook_dir:guard}", wantErr: "hook_dir placeholders are unsupported"},
		{name: "malformed directory id", command: "{hook_dir:../guard}", wantErr: "path-like"},
		{name: "nested placeholder", command: "{hook_file:{hook_file:guard}}", wantErr: "placeholder delimiters"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(Spec{
				Name:    "protect",
				Event:   "Stop",
				Type:    TypeCommand,
				Command: test.command,
				Targets: []target.Target{target.TargetCodex},
				Scope:   target.ScopeProject,
			})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("New error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestHookRejectsControlBearingAssetReferences(t *testing.T) {
	for _, id := range []string{
		"guard\x00script",
		"guard\nscript",
		"guard\u0085script",
		"safe\u202etxt",
		string([]byte{'b', 'a', 'd', 0xff}),
	} {
		_, err := New(Spec{
			Name:    "protect",
			Event:   "Stop",
			Type:    TypeCommand,
			Command: "run {hook_file:" + id + "}",
			Targets: []target.Target{target.TargetCodex},
			Scope:   target.ScopeProject,
		})
		if err == nil {
			t.Fatalf("New accepted control-bearing hook asset reference %q", id)
		}
	}
}

func TestHookRenderCommandReplacesOnlyOwnedExactReferences(t *testing.T) {
	value, err := New(Spec{
		Name:    "protect",
		Event:   "Stop",
		Type:    TypeCommand,
		Command: "python {hook_file:guard} --literal '{hook_file:missing}'",
		Targets: []target.Target{target.TargetCodex},
		Scope:   target.ScopeProject,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	references := value.AssetReferences()
	guard := references[0]
	if guard.ID() != "guard" {
		guard = references[1]
	}

	rendered, err := value.RenderCommand(map[AssetReference]string{
		guard: "/managed/{hook_file:missing}",
	})
	if err != nil {
		t.Fatalf("RenderCommand returned error: %v", err)
	}
	want := "python /managed/{hook_file:missing} --literal '{hook_file:missing}'"
	if rendered != want {
		t.Fatalf("RenderCommand = %q, want %q", rendered, want)
	}

	if _, err := value.RenderCommand(map[AssetReference]string{{id: "other"}: "/managed/other"}); err == nil {
		t.Fatal("RenderCommand accepted a reference not owned by Hook")
	}
}
