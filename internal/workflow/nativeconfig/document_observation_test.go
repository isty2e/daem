package nativeconfig

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	journalnamespace "github.com/isty2e/daem/internal/effect/journal/namespace"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestSelectedCodexDocumentResolvesLiteralRootBeforeJoiningItsName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Win32 normalizes parent components before reparse traversal")
	}
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"actual/inner", "actual/codex", "codex", "manifest"} {
		if err := os.MkdirAll(filepath.Join(base, relative), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(base, "actual", "inner"), filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ relative, content string }{
		{"actual/codex/config.toml", "selected = true\n"},
		{"codex/config.toml", "selected = false\n"},
	} {
		if err := os.WriteFile(filepath.Join(base, fixture.relative), []byte(fixture.content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CODEX_HOME", filepath.Join(base, "link")+"/../codex")
	paths, err := daempaths.Resolve(filepath.Join(base, "manifest", "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := output.Parse("@codex-home/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.Close()
	input, present := observation.NativeInput()
	if !present {
		t.Fatal("selected native document was not retained")
	}
	document, _, present := input.Documents.Evidence().Document(address)
	if !present || string(document.Content()) != "selected = true\n" {
		t.Fatal("native supplier read the lexical sibling instead of the host-selected document")
	}
}

func TestSelectedNativeObservationDoesNotReadOtherDeclaredDocuments(t *testing.T) {
	root := t.TempDir()
	paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := declarationmanifest.Decode([]byte(`version = 1
targets = ["codex"]
[[host_config]]
id = "selected"
scope = "project"
path = "selected.json"
source = "source.json"
[[host_config]]
id = "unselected"
scope = "project"
path = "blocked.json"
source = "other-source.json"
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".codex", "selected.json"), []byte(`{"chosen":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".codex", "blocked.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	config := environment.HostConfigs()[0]
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{
		Target: config.Target(), Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax(),
	})
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
	if err != nil {
		t.Fatal(err)
	}
	facts, present := observation.NativeInput()
	if !present || len(facts.Documents.Evidence().Namespaces()) != 1 {
		t.Fatal("selected observation did not retain one native namespace")
	}
	document, _, present := facts.Documents.Evidence().Document(address)
	if !present || string(document.Content()) != `{"chosen":true}` {
		t.Fatal("selected observation did not retain the actual native bytes")
	}
	before, err := epoch.Snapshot()
	if err != nil || !before.Equal(facts.Documents.Evidence().BeforeState()) {
		t.Fatal("selected observation replaced its complete persistence epoch", err)
	}
	if err := observation.Close(); err != nil {
		t.Fatal(err)
	}
	if _, present := observation.NativeInput(); present {
		t.Fatal("closed observation still lends native evidence")
	}
	blocked := environment.HostConfigs()[1]
	blockedPlacement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: blocked.Target(), Scope: blocked.Scope(), Path: blocked.Path(), Ownership: blocked.Ownership(), Syntax: blocked.Syntax()})
	if err != nil {
		t.Fatal(err)
	}
	blockedAddress, _ := aggregate.NewDocumentAddress(blockedPlacement.Target(), blockedPlacement.Scope(), blockedPlacement.Destination())
	if observed, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address, blockedAddress}); err == nil || observed != nil {
		t.Fatal("an actually selected invalid document became a successful partial read", err)
	}
	if _, err := os.Stat(paths.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read-only observation created metadata", err)
	}
}

func TestSelectedNativeObservationRefusesActualGlobalAuthNamespace(t *testing.T) {
	for _, host := range []struct {
		selected   target.Target
		nativePath string
		relative   string
	}{
		{target.TargetCodex, "auth.json", ".codex/auth.json"},
		{target.TargetPi, "agent/auth.json", ".pi/agent/auth.json"},
	} {
		for _, scenario := range []struct {
			name   string
			shared bool
			exists bool
		}{
			{"global auth", true, true},
			{"future global auth", true, false},
			{"unrelated project file", false, true},
		} {
			t.Run(string(host.selected)+"/"+scenario.name, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				if err := os.MkdirAll(home, 0o700); err != nil {
					t.Fatal(err)
				}
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				t.Setenv("CODEX_HOME", "")
				t.Setenv("PI_CODING_AGENT_DIR", "")
				manifestRoot := home
				if !scenario.shared {
					manifestRoot = filepath.Join(root, "project")
				}
				if err := os.MkdirAll(manifestRoot, 0o700); err != nil {
					t.Fatal(err)
				}
				paths, err := daempaths.Resolve(filepath.Join(manifestRoot, "daem.toml"))
				if err != nil {
					t.Fatal(err)
				}
				hostPath := filepath.Join(manifestRoot, host.relative)
				content := []byte("{\"fixture\":\"not-a-real-credential\"}\n")
				if scenario.exists {
					if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(hostPath, content, 0o600); err != nil {
						t.Fatal(err)
					}
				}

				placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{
					Target: host.selected, Scope: target.ScopeProject, Path: host.nativePath, Ownership: "document",
				})
				if err != nil {
					t.Fatal(err)
				}
				address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
				if err != nil {
					t.Fatal(err)
				}
				epoch, err := LoadPersistenceEpoch(t.Context(), paths)
				if err != nil {
					t.Fatal(err)
				}
				observation, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
				if observation != nil {
					t.Cleanup(func() {
						if err := observation.Close(); err != nil {
							t.Error(err)
						}
					})
				}
				if scenario.shared {
					var refusal *journalnamespace.ProtectionError
					if !errors.As(err, &refusal) || refusal.Kind() != journalnamespace.ProtectionReserved || refusal.Host() != host.selected || !errors.Is(err, configuration.ErrReserved) {
						t.Fatal("actual global authentication was not refused by its resource class", err)
					}
					if err == nil || observation != nil {
						fixtureVisible := false
						if observation != nil && scenario.exists {
							if facts, present := observation.NativeInput(); present {
								document, _, present := facts.Documents.Evidence().Document(address)
								fixtureVisible = present && string(document.Content()) == string(content)
							}
						}
						t.Fatalf("actual global auth namespace became an admitted native read (fixture content visible: %t)", fixtureVisible)
					}
				} else {
					if err != nil || observation == nil {
						t.Fatal("an unrelated project basename was reserved", err)
					}
					facts, present := observation.NativeInput()
					if !present {
						t.Fatal("unrelated native evidence is absent")
					}
					document, _, present := facts.Documents.Evidence().Document(address)
					if !present || string(document.Content()) != string(content) {
						t.Fatal("unrelated project bytes changed")
					}
				}
				if _, err := os.Stat(paths.StateDir); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("read-only qualification created metadata", err)
				}
			})
		}
	}
}

func TestNativeProtectionRootsCannotGrantProjectSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	home, project := filepath.Join(root, "home"), filepath.Join(root, "project")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	global := filepath.Join(home, ".codex")
	if err := os.MkdirAll(global, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, "shared.json"), []byte(`{"benign":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(global, filepath.Join(project, ".codex")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	paths, err := daempaths.Resolve(filepath.Join(project, "daem.toml"))
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{
		Target: target.TargetCodex, Scope: target.ScopeProject, Path: "shared.json", Ownership: "document",
	})
	if err != nil {
		t.Fatal(err)
	}
	address, err := aggregate.NewDocumentAddress(placement.Target(), placement.Scope(), placement.Destination())
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := LoadPersistenceEpoch(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := ObserveSelectedDocuments(t.Context(), paths, &epoch, []aggregate.DocumentAddress{address})
	if observation != nil {
		t.Cleanup(func() {
			if err := observation.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err == nil || observation != nil {
		t.Fatal("a protection-only global root granted an escaping project writer")
	}
	var protection *journalnamespace.ProtectionError
	if errors.As(err, &protection) {
		t.Fatal("a false protection hit masked the independent writer-confinement refusal", err)
	}
}
