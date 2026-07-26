package manifest

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
)

func TestResolveSelectedCarrierSourcesCanonicalizesPiLocalSource(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	environment := decodePiExtension(t, "tools", "project", filepath.Join("packages", ".", "tools"))

	resolved, err := ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("packages", "tools")
	if got := resolved.Extensions()[0].Source().Ref(); got != want {
		t.Fatalf("resolved source = %q, want %q", got, want)
	}

	again, err := ResolveSelectedCarrierSources(paths, resolved)
	if err != nil {
		t.Fatal(err)
	}
	if again.Extensions()[0].CarrierKey() != resolved.Extensions()[0].CarrierKey() {
		t.Fatal("carrier source resolution is not idempotent")
	}
}

func TestResolveSelectedCarrierSourcesPreservesSafeArgForDashPrefixedProjectPath(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(root, "-tools")
	sourceURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(sourcePath)}).String()

	resolved, err := ResolveSelectedCarrierSources(
		paths,
		decodePiExtension(t, "tools", "project", sourceURL),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "." + string(filepath.Separator) + "-tools"
	if got := resolved.Extensions()[0].Source().Ref(); got != want {
		t.Fatalf("resolved source = %q, want safe argv spelling %q", got, want)
	}
}

func TestResolveSelectedCarrierSourcesRejectsSameScopePiAliases(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(root, "packages", "tools")
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(canonical)}).String()
	environment, err := Decode(fmt.Appendf(nil, `version = 1
targets = ["pi"]

[[extension]]
id = "relative"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = %q }

[[extension]]
id = "url"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = %q }
`, filepath.Join("packages", "tools"), fileURL))
	if err != nil {
		t.Fatal(err)
	}

	_, err = ResolveSelectedCarrierSources(paths, environment)
	if err == nil || !strings.Contains(err.Error(), "duplicate extension relation") {
		t.Fatalf("ResolveSelectedCarrierSources() error = %v, want duplicate relation rejection", err)
	}
}

func TestResolveSelectedCarrierSourcesKeepsScopeAndNonLocalIdentityIndependent(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := Decode([]byte(`version = 1
targets = ["pi"]

[[extension]]
id = "project-tools"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "./packages/tools" }

[[extension]]
id = "global-tools"
carrier = "pi-package"
targets = ["pi"]
scope = "global"
source = { host_source = "./packages/tools" }

[[extension]]
id = "npm-tools"
carrier = "pi-package"
targets = ["pi"]
scope = "global"
source = { host_source = "npm:@acme/tools@1.2.3" }
`))
	if err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveSelectedCarrierSources(paths, environment)
	if err != nil {
		t.Fatal(err)
	}
	extensions := resolved.Extensions()
	wantProject := filepath.Join("packages", "tools")
	wantGlobal := filepath.Join(root, "packages", "tools")
	if extensions[0].Source().Ref() != wantProject || extensions[1].Source().Ref() != wantGlobal {
		t.Fatalf(
			"local sources = %q, %q, want project %q and global %q",
			extensions[0].Source().Ref(),
			extensions[1].Source().Ref(),
			wantProject,
			wantGlobal,
		)
	}
	if extensions[0].Scope() != target.ScopeProject || extensions[1].Scope() != target.ScopeGlobal {
		t.Fatalf("resolved scopes = %q, %q", extensions[0].Scope(), extensions[1].Scope())
	}
	if extensions[0].CarrierKey() == extensions[1].CarrierKey() {
		t.Fatal("project and global carrier identities collapsed")
	}
	if got := extensions[2].Source().Ref(); got != "npm:@acme/tools@1.2.3" {
		t.Fatalf("non-local source changed to %q", got)
	}
}

func TestResolveSelectedCarrierSourcesConvergesCrossProjectGlobalAliases(t *testing.T) {
	shared := filepath.Join(t.TempDir(), "packages", "tools")
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	firstPaths, err := daempaths.Resolve(filepath.Join(firstRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	secondPaths, err := daempaths.Resolve(filepath.Join(secondRoot, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(firstRoot, shared)
	if err != nil {
		t.Fatal(err)
	}
	fileURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(shared)}).String()

	first, err := ResolveSelectedCarrierSources(
		firstPaths,
		decodePiExtension(t, "first", "global", relative),
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ResolveSelectedCarrierSources(
		secondPaths,
		decodePiExtension(t, "second", "global", fileURL),
	)
	if err != nil {
		t.Fatal(err)
	}
	if first.Extensions()[0].CarrierKey() != second.Extensions()[0].CarrierKey() {
		t.Fatalf(
			"cross-project aliases resolved to distinct keys: %q and %q",
			first.Extensions()[0].Source().Ref(),
			second.Extensions()[0].Source().Ref(),
		)
	}
}

func TestLoadSelectedResolvesPiLocalSourceAgainstManifestRoot(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	if err := os.WriteFile(manifestPath, []byte(`version = 1
targets = ["pi"]

[[extension]]
id = "tools"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "../packages/tools" }
`), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		t.Fatal(err)
	}

	environment, err := LoadSelected(paths)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Clean(filepath.Join("..", "packages", "tools"))
	if got := environment.Extensions()[0].Source().Ref(); got != want {
		t.Fatalf("LoadSelected source = %q, want %q", got, want)
	}
}

func decodePiExtension(t *testing.T, id string, scope string, source string) desired.Environment {
	t.Helper()
	environment, err := Decode(fmt.Appendf(nil, `version = 1
targets = ["pi"]

[[extension]]
id = %q
carrier = "pi-package"
targets = ["pi"]
scope = %q
source = { host_source = %q }
`, id, scope, source))
	if err != nil {
		t.Fatal(err)
	}
	return environment
}
