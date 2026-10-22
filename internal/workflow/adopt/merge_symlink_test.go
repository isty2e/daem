package adopt

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/target"
)

func TestMergePreservesSelectedManifestLink(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	for _, name := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
		t.Setenv("XDG_"+name+"_HOME", filepath.Join(root, name))
	}
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "pi-global"))
	settings := []byte(`{"packages":["npm:@acme/tools@1.2.3"]}`)
	writeMergeFixture(t, filepath.Join(root, ".pi", "settings.json"), settings)
	writeMergeFixture(t, filepath.Join(root, "dot", "manifest.toml"), []byte("version = 1\ntargets = [\"pi\"]\n"))
	output := filepath.Join(root, "daem.toml")
	if err := os.Symlink("dot/manifest.toml", output); err != nil {
		t.Fatal(err)
	}
	sourceDir, err := adoptmodel.NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := adoptmodel.NewRequest([]target.Target{target.TargetPi}, []target.Scope{target.ScopeProject}, output, sourceDir, true)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := buildPlanForTest(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	executed, err := ExecuteCommandPlan(t.Context(), commandPlanWithRevisionEvidence(t, request, plan), nil)
	if err != nil {
		t.Fatal(err)
	}
	if executed.AdoptionPlan().ResourceCount() != 1 || executed.AdoptionPlan().Extensions()[0].Source().Ref() != "npm:@acme/tools@1.2.3" {
		t.Fatal("merge lost the selected project extension")
	}
	if link, err := os.Readlink(output); err != nil || link != "dot/manifest.toml" {
		t.Fatalf("merge replaced the selected link: %q, %v", link, err)
	}
	if !bytes.Equal(readMergeFixture(t, filepath.Join(root, ".pi/settings.json")), settings) {
		t.Fatal("merge changed host configuration")
	}
	if !bytes.Equal(readMergeFixture(t, output), executed.AdoptionPlan().ManifestContent()) {
		t.Fatal("merge did not update the actual manifest")
	}
}

func writeMergeFixture(t testing.TB, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readMergeFixture(t testing.TB, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}
