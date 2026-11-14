package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	daempaths "github.com/isty2e/daem/internal/paths"
)

func TestCaptureSharingSelectorsRequireAQualifiedDirectChild(t *testing.T) {
	for _, test := range []struct {
		name      string
		child     string
		existing  string
		exclude   string
		wantGroup bool
	}{
		{name: "directory itself is shared", exclude: "skills", wantGroup: true},
		{name: "missing stable prefix", child: "Kept/Café/settings.jsonc", exclude: "settings.jsonc", wantGroup: true},
		{name: "missing stable prefix is excluded", child: "Kept/Café/settings.jsonc", exclude: "Kept"},
		{name: "existing direct child", child: "Kept/Café/settings.jsonc", existing: "Kept", exclude: "Kept"},
		{name: "unknown normalization of direct child", child: "Café/settings.jsonc", existing: ".", exclude: "Cafe\u0301", wantGroup: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			if test.existing != "" {
				if err := os.MkdirAll(filepath.Join(root, "skills", test.existing), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			manifest := []byte("version = 1\ntargets = ['pi']\n[[skill_group]]\nsource = { path = 'skills', mode = 'vendor' }\ninclude = ['glob:*']\nscope = 'project'\n")
			if test.exclude != "" {
				manifest = fmt.Appendf(manifest, "exclude = ['glob:%s']\n", test.exclude)
			}
			environment, err := declarationmanifest.Decode(manifest)
			if err != nil {
				t.Fatal(err)
			}
			selected := filepath.Join(root, "skills", test.child)
			budget, err := physicalwork.NewBudget(0)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := captureReferent(selected, budget)
			if err != nil {
				t.Fatal(err)
			}
			_, provisional := observed.observed.Provisional()
			references, err := captureSourceReferences(t.Context(), daempaths.Paths{ManifestRoot: root}, environment, selected, budget)
			if test.name == "unknown normalization of direct child" && provisional {
				if err == nil {
					t.Fatal("unknown child spelling silently applied a byte-oriented selector")
				}
				return
			}
			if err != nil || (len(references) == 1) != test.wantGroup {
				t.Fatal("incorrect sharing disclosure", references, err)
			}
		})
	}
}

func TestCaptureSharingDisclosureUsesDirectorySelectorsAndLocalCarrierOwner(t *testing.T) {
	fixture := newCaptureWorkflowFixture(t, "keys", []byte(`{"nested":{"a":2}}`))
	directory := filepath.Join(fixture.paths.ManifestRoot, "skills", "Kept")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(fixture.source, filepath.Join(directory, "settings.jsonc")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("skills/Kept/settings.jsonc", fixture.source); err != nil {
		t.Fatal(err)
	}
	manifest := append(fixture.manifest, []byte(`
[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
scope = "project"
[[skill_group]]
source = { path = "skills", mode = "vendor" }
include = ["glob:*"]
exclude = ["glob:Kept"]
scope = "project"
[[extension]]
id = "local-carrier"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "./skills/Kept" }
`)...)
	if err := os.WriteFile(fixture.paths.ManifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	var group, excluded, carrier bool
	for _, reference := range plan.KnownReferences() {
		group = group || reference.Resource == "skill_group[0]"
		excluded = excluded || reference.Resource == "skill_group[1]"
		carrier = carrier || strings.Contains(reference.Resource, "local-carrier")
	}
	if !group || excluded || !carrier || len(plan.KnownReferences()) != 4 {
		t.Fatal("sharing disclosure missed aliases/selected directory readers or included an excluded child", plan.KnownReferences())
	}
	if result, err := ExecuteHostConfigCapture(t.Context(), plan, plan.ApprovalPath()); err != nil || !result.Committed {
		t.Fatal("capture could not lease the disclosed source-directory aliases", result, err)
	}
}
