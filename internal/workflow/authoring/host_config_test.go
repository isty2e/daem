package authoring

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

func TestHostConfigDeclarationEditing(t *testing.T) {
	const header = "version = 1\ntargets = [\"codex\"]\n"
	const inlineSelected = `{ id = "remove", path = "prefs.toml", source = { git = "https://example.org/settings.git", path = "a,b.toml", ref = "main" } }`
	const inlineRetained = `{ id = "keep", path = "config.toml", source = "keep.toml" }`
	const blockSelected = "[[host_config]]\nid = \"remove\"\npath = \"prefs.toml\"\n[host_config.source]\ngit = \"https://example.org/settings.git\"\npath = \"a,b.toml\"\nref = \"main\"\n"
	const blockRetained = "[[host_config]]\nid = \"keep\"\npath = \"config.toml\"\nsource = \"keep.toml\"\n"
	const separator = "# keep this note, even when it follows the removed row"

	for _, example := range []struct{ name, original, retained string }{
		{"array tables", header + blockSelected + separator + "\n" + blockRetained, blockRetained},
		{"first inline row", header + "\"host_config\" = [\n  " + inlineSelected + ",\n  " + separator + "\n  " + inlineRetained + "\n]\n", inlineRetained},
		{"last inline row", header + "host_config = [\n  " + inlineRetained + "\n  " + separator + "\n  , " + inlineSelected + "]\n", inlineRetained},
		{"only inline row", header + "host_config = [" + inlineSelected + ",\n" + separator + "\n]\n", ""},
	} {
		t.Run(example.name, func(t *testing.T) {
			root := t.TempDir()
			document := ManifestDocument{Root: root, Content: []byte(example.original)}
			request := RemoveHostConfigRequest{ID: "remove", Target: target.TargetCodex, Scope: target.ScopeProject}
			change, err := buildRemoveHostConfigChange(document, request)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(change.Content, []byte(separator)) || !bytes.Contains(change.Content, []byte(example.retained)) {
				t.Fatalf("edited manifest lost unselected bytes:\n%s", change.Content)
			}
			document.Content = change.Content
			add := AddHostConfigRequest{
				ID: "new, quoted \"name\"", SourceArg: filepath.Join(root, "prefs\a, with settings.toml"),
				Target: target.TargetCodex, Scope: target.ScopeProject, HostPath: "prefs.toml",
			}
			change, err = buildAddHostConfigChange(document, add)
			if err != nil {
				t.Fatal(err)
			}
			environment, err := declarationmanifest.Decode(change.Content)
			if err != nil {
				t.Fatal(err)
			}
			id, _ := entity.New(entity.KindHostConfig, add.ID)
			configs := environment.HostConfigs()
			wantCount := 2
			if example.retained == "" {
				wantCount = 1
			}
			if index := hostConfigIndex(configs, id); index < 0 || len(configs) != wantCount || !bytes.Contains(change.Content, []byte(separator)) {
				t.Fatalf("unexpected manifest after remove and add:\n%s", change.Content)
			}
			document.Content = change.Content
			again, err := buildAddHostConfigChange(document, add)
			if err != nil || again.ChangeKind != "unchanged" || !bytes.Equal(again.Content, change.Content) {
				t.Fatalf("satisfied add did not preserve declaration: %v, %q", err, again.ChangeKind)
			}
		})
	}
}

func TestHostConfigDeclarationSelectionUsesEffectiveFacts(t *testing.T) {
	root := t.TempDir()
	content := []byte(`version=1
targets=['codex']
host_config = [{ id = "sample", path = "config.toml", source = "./prefs.toml" }]
`)
	document := ManifestDocument{Root: root, Content: content}
	add := AddHostConfigRequest{ID: "sample", SourceArg: filepath.Join(root, "prefs.toml"), Target: target.TargetCodex, Scope: target.ScopeProject, HostPath: "config.toml"}
	change, err := buildAddHostConfigChange(document, add)
	if err != nil || change.ChangeKind != "unchanged" || !bytes.Equal(change.Content, content) {
		t.Fatalf("an inherited declaration did not satisfy add: %v, %q", err, change.ChangeKind)
	}
	add.Syntax = hostconfig.SyntaxTOML
	if _, err := buildAddHostConfigChange(document, add); err == nil {
		t.Fatal("an explicit syntax unexpectedly replaced the default syntax intent")
	}
	_, err = buildRemoveHostConfigChange(document, RemoveHostConfigRequest{ID: "sample", Scope: target.ScopeGlobal})
	var missing *ResourceSelectionNotFoundError
	if !errors.As(err, &missing) || len(missing.AvailableSelections()) != 1 || missing.AvailableSelections()[0].Scope != "project" {
		t.Fatalf("remove did not refuse the mismatched scope with a project alternative: %v", err)
	}
}

func TestHostConfigAuthoringSourceFamilies(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	for _, example := range []struct {
		name     string
		request  AddHostConfigRequest
		wantKind source.SourceKind
		wantID   string
	}{
		{"relative local with colon", AddHostConfigRequest{SourceArg: "./prefs:v2.json", Scope: target.ScopeProject}, source.SourceKindLocal, "local:prefs:v2.json?mode=vendor"},
		{"no local existence heuristic", AddHostConfigRequest{SourceArg: "owner/repo/prefs.json", Scope: target.ScopeProject}, source.SourceKindLocal, "local:owner/repo/prefs.json?mode=vendor"},
		{"local Git repository", AddHostConfigRequest{SourceArg: root, SourcePath: "prefs.toml", Ref: "refs/heads/main"}, source.SourceKindGit, "ref=branch%3Amain"},
		{"shorthand Git", AddHostConfigRequest{SourceArg: "owner/repo/prefs.toml", Ref: "main"}, source.SourceKindGit, "&path=prefs.toml&ref=name%3Amain"},
		{"versioned S3", AddHostConfigRequest{SourceArg: "s3://fixture/prefs.json", VersionID: "opaque-version", Region: "us-east-1"}, source.SourceKindS3, "s3:s3://fixture/prefs.json?region=us-east-1&version_id=opaque-version"},
		{"reject unsupported scheme", AddHostConfigRequest{SourceArg: "http://example.org/configs", SourcePath: "prefs.json", Ref: "main"}, "", ""},
		{"reject implicit Git ref", AddHostConfigRequest{SourceArg: "https://example.org/configs"}, "", ""},
		{"reject Git root", AddHostConfigRequest{SourceArg: "https://example.org/configs", Ref: "main"}, "", ""},
		{"reject conflicting Git paths", AddHostConfigRequest{SourceArg: "owner/repo/settings.toml", SourcePath: "other.toml", Ref: "main"}, "", ""},
		{"reject S3 with Git options", AddHostConfigRequest{SourceArg: "s3://fixture/prefs.json", Ref: "main"}, "", ""},
		{"reject local with S3 options", AddHostConfigRequest{SourceArg: "prefs.json", VersionID: "opaque-version"}, "", ""},
	} {
		t.Run(example.name, func(t *testing.T) {
			provenance, err := hostConfigAuthoringSource(example.request, root)
			if example.wantKind == "" {
				if err == nil {
					t.Fatalf("invalid request admitted as %q", provenance.Kind())
				}
				return
			}
			if err != nil || provenance.Kind() != example.wantKind {
				t.Fatalf("source kind = %q, error = %v", provenance.Kind(), err)
			}
			id, err := source.SourceIDFor(provenance)
			if err != nil || !strings.HasSuffix(string(id), example.wantID) {
				t.Fatalf("source identity = %q, error = %v, want suffix %q", id, err, example.wantID)
			}
		})
	}
}

func TestHostConfigAuthoringRejectsControlFileSource(t *testing.T) {
	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	lockPath := filepath.Join(root, "daem.lock.toml")
	manifest := []byte("version = 1\ntargets = ['codex']\n")
	lock := []byte("version = 7\n[locked]\n")
	for path, content := range map[string][]byte{manifestPath: manifest, lockPath: lock} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sources := []string{manifestPath, lockPath}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err == nil {
		sources = append(sources, filepath.Join(alias, "daem.toml"))
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		t.Fatal(err)
	}

	for _, mode := range []AuthoringMode{AuthoringModeDryRun, AuthoringModeWrite} {
		for _, path := range sources {
			_, err := AddHostConfig(t.Context(), ExecutionOptions{ManifestPath: manifestPath, Mode: mode}, AddHostConfigRequest{
				ID: "self-source", SourceArg: path, Target: target.TargetCodex, Scope: target.ScopeProject, HostPath: "prefs.toml",
			})
			if err == nil || !strings.Contains(err.Error(), "use a separate source file") {
				t.Fatalf("%s accepted control file source %q: %v", mode, path, err)
			}
		}
	}
	for path, want := range map[string][]byte{manifestPath: manifest, lockPath: lock} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, want) {
			t.Fatalf("rejected source changed control file %q: %v, %q", path, err, actual)
		}
	}
}
