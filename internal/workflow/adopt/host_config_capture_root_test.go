package adopt

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	codexhostpath "github.com/isty2e/daem/internal/output/hostpath/codex"
)

func TestCaptureRefusesSourceAliasingUnselectedHostThroughLiteralRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 normalizes parent components before reparse traversal")
	}
	host := []byte(`{"nested":{"a":42}}`)
	fixture := newCaptureWorkflowFixture(t, "keys", host)
	root := fixture.paths.ManifestRoot
	actual := filepath.Join(root, "actual", "native")
	for _, path := range []string{filepath.Join(root, "actual", "inner"), actual, filepath.Join(root, "native")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "literal")
	if err := os.Symlink(filepath.Join(root, "actual", "inner"), link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(codexhostpath.HomeEnvironmentVariable, link+"/../native")
	fixture.host = filepath.Join(actual, "capture.jsonc")
	fixture.source = filepath.Join(actual, "unselected.json")
	source := []byte(`{"nested":{"a":1,"missing":2},"unselected":7}`)
	if err := os.WriteFile(fixture.host, host, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.source, source, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := strings.ReplaceAll(string(fixture.manifest), `scope = "project"`, `scope = "global"`)
	manifest = strings.ReplaceAll(manifest, `source = "source.jsonc"`, `source = "`+fixture.source+`"`)
	manifest = strings.ReplaceAll(manifest, `source = "alias.jsonc"`, `source = "`+fixture.alias+`"`)
	valid := strings.ReplaceAll(manifest, fixture.source, filepath.Join(root, "source.jsonc"))
	if err := os.WriteFile(fixture.paths.ManifestPath, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	control, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
	if err != nil {
		t.Fatal("literal root could not qualify an independent named source", err)
	}
	if err := control.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.paths.ManifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := BuildHostConfigUpdatePlan(t.Context(), HostConfigUpdateInput{Paths: fixture.paths, Name: "chosen"})
	if plan != nil {
		defer plan.Close()
	}
	if !errors.Is(err, ErrHostConfigCaptureArtifactOverlap) || plan != nil {
		t.Fatal("capture admitted a source write to the actual unselected host output", err)
	}
	assertCaptureArtifact(t, fixture.source, source)
	assertCaptureArtifact(t, fixture.host, host)
	assertCaptureArtifact(t, fixture.paths.ManifestPath, []byte(manifest))
}
