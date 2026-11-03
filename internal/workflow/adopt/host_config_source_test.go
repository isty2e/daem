package adopt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
