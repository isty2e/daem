package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/workflow/nativeconfig"
)

func prepareHostConfigUnmanageCLI(t *testing.T) (daempaths.Paths, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, variable := range []string{"CODEX_HOME", "PI_CODING_AGENT_DIR", "CLAUDE_CONFIG_DIR", "OPENCODE_CONFIG_DIR"} {
		t.Setenv(variable, "")
	}
	for _, variable := range []string{"XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, filepath.Join(home, variable))
	}
	manifest := filepath.Join(home, "daem.toml")
	source := filepath.Join(home, "preferences.toml")
	if err := os.WriteFile(source, []byte("model='declared'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='global'\npath='config.toml'\nsource=%q\nownership='keys'\n", filepath.ToSlash(source))
	if err := os.WriteFile(manifest, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := daempaths.Resolve(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return paths, source, filepath.Join(home, ".codex", "config.toml")
}

func runHostConfigUnmanageCLI(t *testing.T, args ...string) (int, []byte) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := RunWithOptions(args, RunOptions{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Logf("exit=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	return code, stdout.Bytes()
}

func TestCLIUnmanageHostConfigKeepsDriftWithoutSourceDeclarationOrLock(t *testing.T) {
	for _, scenario := range []struct {
		name              string
		removeDeclaration bool
		keepLock          bool
	}{
		{name: "missing-source-and-lock"},
		{name: "missing-declaration-source-and-lock", removeDeclaration: true},
		{name: "locked-subjects-with-missing-source", keepLock: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			paths, source, host := prepareHostConfigUnmanageCLI(t)
			for _, args := range [][]string{{"lock", "--manifest", paths.ManifestPath}, {"apply", "--manifest", paths.ManifestPath, "--yes"}} {
				if code, _ := runHostConfigUnmanageCLI(t, args...); code != 0 {
					t.Fatal("fixture could not establish real native management")
				}
			}
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			if !scenario.keepLock {
				if err := os.Remove(paths.LockfilePath); err != nil {
					t.Fatal(err)
				}
			}
			if scenario.removeDeclaration {
				if err := os.Remove(paths.ManifestPath); err != nil {
					t.Fatal(err)
				}
			}
			drift := []byte("model='user-drift'\n# keep this comment\nunmanaged=true\n")
			if err := os.WriteFile(host, drift, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, preview := range []bool{true, false} {
				args := []string{"unmanage", "host-config", "preferences", "--manifest", paths.ManifestPath, "--json"}
				if preview {
					args = append(args, "--dry-run")
				}
				code, content := runHostConfigUnmanageCLI(t, args...)
				if code != 0 {
					t.Fatal("native unmanage CLI failed")
				}
				var result struct {
					Command    string `json:"command"`
					Management struct {
						Status string `json:"status"`
					} `json:"management"`
					Host struct {
						State string `json:"state"`
					} `json:"host"`
					Changes []struct {
						Resource struct {
							Kind string `json:"kind"`
						} `json:"resource"`
					} `json:"changes"`
				}
				want := "released"
				if preview {
					want = "would_release"
				}
				if err := json.Unmarshal(content, &result); err != nil || result.Command != "unmanage" || result.Management.Status != want || result.Host.State != "retained" || len(result.Changes) != 1 || result.Changes[0].Resource.Kind != "host_config" {
					t.Fatal("unmanage lost its structured outcome", string(content), err)
				}
				current, err := os.ReadFile(host)
				if err != nil || !bytes.Equal(current, drift) {
					t.Fatal("unmanage altered current host bytes", err)
				}
			}
			state, err := statefile.LoadOptional(t.Context(), paths.StatefilePath)
			if err != nil || state.ContainsHostConfig() {
				t.Fatal("unmanage retained native management", err)
			}
			store, err := ownershipstore.New(paths.OwnershipRegistryPath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil || len(registry.Claims()) != 0 || len(registry.DocumentBindings()) != 0 {
				t.Fatal("unmanage retained own metadata", err)
			}
			absent := []string{source}
			if !scenario.keepLock {
				absent = append(absent, paths.LockfilePath)
			}
			for _, path := range absent {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("unmanage recreated a missing artifact", path, err)
				}
			}
			if scenario.keepLock {
				codecs, err := nativeconfig.Codecs(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				codec, err := lockfile.NewCodec(codecs.Contributions())
				if err != nil {
					t.Fatal(err)
				}
				locked, err := codec.Load(t.Context(), paths.LockfilePath)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := entity.New(entity.KindHostConfig, "preferences")
				if _, present := locked.Locked.ExactSupplySubject(id); present {
					t.Fatal("release retained its default-lock subjects")
				}
			}
			if scenario.removeDeclaration {
				if _, err := os.Stat(paths.ManifestPath); !os.IsNotExist(err) {
					t.Fatal("unmanage recreated a missing declaration", err)
				}
			}
		})
	}
}

func TestCLIUnmanageUnappliedHostConfigDoesNotCreateManagement(t *testing.T) {
	paths, source, host := prepareHostConfigUnmanageCLI(t)
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	code, content := runHostConfigUnmanageCLI(t, "unmanage", "host-config", "preferences", "--manifest", paths.ManifestPath)
	if code != 0 || !bytes.Contains(content, []byte("host: retained")) {
		t.Fatal("declaration-only unmanage failed", string(content))
	}
	for _, path := range []string{host, paths.StatefilePath, paths.LockfilePath, paths.OwnershipRegistryPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("authoring-only unmanage manufactured management or a host output", path, err)
		}
	}
}

func TestCLIUnmanageHostConfigRefusesAnUneditableDeclarationBeforeReleasingState(t *testing.T) {
	paths, source, host := prepareHostConfigUnmanageCLI(t)
	for _, args := range [][]string{{"lock", "--manifest", paths.ManifestPath}, {"apply", "--manifest", paths.ManifestPath, "--yes"}} {
		if code, _ := runHostConfigUnmanageCLI(t, args...); code != 0 {
			t.Fatal("fixture could not establish management")
		}
	}
	inline := fmt.Sprintf("version=1\ntargets=['codex']\nhost_config=[{id='preferences',scope='global',path='config.toml',source=%q,ownership='keys'}]\n", filepath.ToSlash(source))
	if err := os.WriteFile(paths.ManifestPath, []byte(inline), 0o600); err != nil {
		t.Fatal(err)
	}
	before := make(map[string][]byte)
	for _, path := range []string{paths.ManifestPath, paths.LockfilePath, paths.StatefilePath, paths.OwnershipRegistryPath, host} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		before[path] = content
	}
	if code, _ := runHostConfigUnmanageCLI(t, "unmanage", "host-config", "preferences", "--manifest", paths.ManifestPath); code != 1 {
		t.Fatal("unselectable source edit did not refuse")
	}
	for path, expected := range before {
		if content, err := os.ReadFile(path); err != nil || !bytes.Equal(content, expected) {
			t.Fatal("unselectable declaration released or changed data", path, err)
		}
	}
	if err := os.WriteFile(paths.ManifestPath, []byte("version=1\ntargets=['codex']\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if code, _ := runHostConfigUnmanageCLI(t, "unmanage", "host-config", "preferences", "--manifest", paths.ManifestPath); code != 0 {
		t.Fatal("retained management could not be released after manual declaration removal")
	}
	if content, err := os.ReadFile(host); err != nil || !bytes.Equal(content, before[host]) {
		t.Fatal("release after manual removal changed host data", err)
	}
}

func TestCLIUnmanageHostConfigWholeDocumentUsesTheSelectedNativeRoot(t *testing.T) {
	paths, _, _ := prepareHostConfigUnmanageCLI(t)
	root := filepath.Join(filepath.Dir(paths.ManifestPath), "selected-opencode")
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(filepath.Dir(paths.ManifestPath), "additional-opencode"))
	source := filepath.Join(filepath.Dir(paths.ManifestPath), "preferences.jsonc")
	if err := os.WriteFile(source, []byte("{\"theme\":\"declared\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("version=1\ntargets=['opencode']\n[[host_config]]\nid='preferences'\nscope='global'\npath='preferences.jsonc'\nsource=%q\nownership='document'\n", filepath.ToSlash(source))
	if err := os.WriteFile(paths.ManifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"lock", "--manifest", paths.ManifestPath}, {"apply", "--manifest", paths.ManifestPath, "--yes"}} {
		if code, _ := runHostConfigUnmanageCLI(t, args...); code != 0 {
			t.Fatal("whole-document fixture could not establish management")
		}
	}
	host := filepath.Join(root, "opencode", "preferences.jsonc")
	if _, err := os.Stat(host); err != nil {
		t.Fatal("apply did not use the overridden native root", err)
	}
	drift := []byte("// user document\n{\"theme\":\"changed\",\"unmanaged\":true}\n")
	if err := os.WriteFile(host, drift, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	if code, _ := runHostConfigUnmanageCLI(t, "unmanage", "host-config", "preferences", "--manifest", paths.ManifestPath); code != 0 {
		t.Fatal("whole-document release failed")
	}
	if content, err := os.ReadFile(host); err != nil || !bytes.Equal(content, drift) {
		t.Fatal("whole-document release changed user content", err)
	}
	snapshot, err := statefile.LoadOptional(t.Context(), paths.StatefilePath)
	if err != nil || snapshot.ContainsHostConfig() {
		t.Fatal("whole-document release retained management", err)
	}
}

func TestCLIUnmanageHostConfigKeepsLinkedControlFilesAndTheOriginalOwner(t *testing.T) {
	paths, source, host := prepareHostConfigUnmanageCLI(t)
	for _, args := range [][]string{{"lock", "--manifest", paths.ManifestPath}, {"apply", "--manifest", paths.ManifestPath, "--yes"}} {
		if code, _ := runHostConfigUnmanageCLI(t, args...); code != 0 {
			t.Fatal("fixture could not establish management")
		}
	}
	referents := t.TempDir()
	for _, selected := range []string{paths.ManifestPath, paths.LockfilePath} {
		referent := filepath.Join(referents, filepath.Base(selected))
		if err := os.Rename(selected, referent); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(referent, selected); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	hostBefore, err := os.ReadFile(host)
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := runHostConfigUnmanageCLI(t, "unmanage", "host-config", "preferences", "--manifest", paths.ManifestPath); code != 0 {
		t.Fatal("linked metadata release failed")
	}
	for _, selected := range []string{paths.ManifestPath, paths.LockfilePath} {
		if info, err := os.Lstat(selected); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("release replaced a selected control link", err)
		}
	}
	snapshot, err := statefile.LoadOptional(t.Context(), paths.StatefilePath)
	if err != nil || snapshot.ContainsHostConfig() {
		t.Fatal("release lost the original manifest-root State", err)
	}
	codecs, err := nativeconfig.Codecs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	codec, err := lockfile.NewCodec(codecs.Contributions())
	if err != nil {
		t.Fatal(err)
	}
	locked, err := codec.Load(t.Context(), paths.LockfilePath)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := entity.New(entity.KindHostConfig, "preferences")
	if _, present := locked.Locked.ExactSupplySubject(id); present {
		t.Fatal("linked lock retained released subjects")
	}
	if content, err := os.ReadFile(host); err != nil || !bytes.Equal(content, hostBefore) {
		t.Fatal("linked metadata release changed the host", err)
	}
}
