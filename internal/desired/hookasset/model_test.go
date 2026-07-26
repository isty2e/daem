package hookasset

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
)

func TestHookAssetConstructorOwnsFamilyInvariants(t *testing.T) {
	asset, err := New(Spec{
		Name:         "guard",
		Source:       sourcetest.Local(t, "hooks/guard.sh", source.LocalSourceModeVendor),
		ArtifactKind: ArtifactKindFile,
		Scope:        target.ScopeProject,
		Executable:   true,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if asset.ID().Kind() != entity.KindHookAsset || asset.ID().Name() != "guard" || !asset.Executable() || asset.Validate() != nil {
		t.Fatalf("asset = %#v", asset)
	}
}

func TestHookAssetConstructorRejectsInvalidAxesAndZero(t *testing.T) {
	base := Spec{
		Name:         "guard",
		Source:       sourcetest.Local(t, "hooks/guard.sh", source.LocalSourceModeVendor),
		ArtifactKind: ArtifactKindFile,
		Scope:        target.ScopeProject,
	}
	tests := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{name: "path id", edit: func(spec *Spec) { spec.Name = "../guard" }, want: "path-like"},
		{name: "bad source", edit: func(spec *Spec) { spec.Source = source.Source{} }, want: "unsupported source"},
		{name: "bad kind", edit: func(spec *Spec) { spec.ArtifactKind = "directory" }, want: "unsupported hook asset kind"},
		{name: "bad scope", edit: func(spec *Spec) { spec.Scope = "workspace" }, want: "unknown scope"},
		{name: "global relative", edit: func(spec *Spec) { spec.Scope = target.ScopeGlobal }, want: "absolute path"},
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
	if err := (HookAsset{}).Validate(); err == nil {
		t.Fatal("zero HookAsset validated")
	}
}

func TestHookAssetNameRejectsEmbeddedControlCharacters(t *testing.T) {
	for _, id := range []string{"guard\x00script", "guard\nscript", "guard\u0085script", "safe\u202etxt", "guard\xffscript"} {
		if err := ValidateName(id); err == nil {
			t.Fatalf("ValidateName(%q) returned nil error", id)
		}
	}
}
