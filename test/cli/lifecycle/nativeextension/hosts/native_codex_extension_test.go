//go:build darwin || linux

package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/statefile"
	clipkg "github.com/isty2e/daem/internal/cli"
	"github.com/isty2e/daem/internal/output/ownership"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/testkit"
	"github.com/isty2e/daem/test/testkit/nativefixture"
)

type nativeCodexFixture struct {
	workspace, manifest, config, keyFile, calls, before, launchState, launchRegistry, payload string
	withoutPlugin, withPlugin                                                                 string
}

func newNativeCodexFixture(t *testing.T, scope target.Scope, fault string) nativeCodexFixture {
	t.Helper()
	return newNativeCodexFixtureWithHistory(t, scope, fault, true)
}

func newNativeCodexFixtureWithHistory(t *testing.T, scope target.Scope, fault string, previouslyInstalled bool) nativeCodexFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	nativefixture.SetRecoveryEnvironment(t, root)
	workspace, home, bin := filepath.Join(root, "workspace"), os.Getenv("CODEX_HOME"), filepath.Join(root, "bin")
	fixture := nativeCodexFixture{workspace: workspace, manifest: filepath.Join(workspace, "daem.toml"), config: filepath.Join(home, "config.toml"), calls: filepath.Join(root, "calls"), before: filepath.Join(root, "before.toml"), launchState: filepath.Join(root, "launch-state.json"), launchRegistry: filepath.Join(root, "launch-registry.json"), payload: filepath.Join(root, "payload.toml")}
	fixture.keyFile = fixture.config
	if scope == target.ScopeProject {
		fixture.keyFile = filepath.Join(workspace, ".codex", "config.toml")
	}
	fixture.withoutPlugin = "# retained root note\nforeign = 'retained'\n[plugins.\"sibling@openai-primary-runtime\"]\nenabled = true\n# retained sibling note\n"
	fixture.withPlugin = fixture.withoutPlugin + "[plugins.\"documents@openai-primary-runtime\"]\nenabled = true\n"
	testkit.WriteFile(t, workspace, "daem.toml", "version=1\ntargets=['codex']\n[[extension]]\nid='documents'\ncarrier='codex-plugin'\nscope='global'\nsource={marketplace='documents@openai-primary-runtime'}\n")
	testkit.WriteFile(t, home, "config.toml", fixture.withoutPlugin)
	testkit.WriteFile(t, root, "payload.toml", fixture.withPlugin)
	script := `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf 'codex-cli 0.145.0\n'; exit 0; fi
[ "$1" = plugin ] || exit 97
operation="$2"
if [ "$operation" = marketplace ]; then
  [ "$#" -eq 5 ] && [ "$3" = upgrade ] && [ "$4" = openai-primary-runtime ] && [ "$5" = --json ] || exit 97
  operation=refresh
else
  [ "$#" -eq 4 ] && [ "$3" = documents@openai-primary-runtime ] && [ "$4" = --json ] || exit 97
  [ "$operation" = add ] || [ "$operation" = remove ] || exit 97
fi
/bin/cp "$CODEX_HOME/config.toml" "$DAEM_TEST_CODEX_BEFORE"
printf '%s\n' "$operation" >> "$DAEM_TEST_CODEX_CALLS"
if [ "$DAEM_TEST_CODEX_PHASE" = native ]; then
  /bin/cp .daem/state.json "$DAEM_TEST_CODEX_STATE"
  /bin/cp "$DAEM_TEST_CODEX_REGISTRY" "$DAEM_TEST_CODEX_LAUNCH_REGISTRY"
  /bin/cp "$DAEM_TEST_CODEX_KEY_FILE" "$DAEM_TEST_CODEX_KEY_BEFORE"
fi
if [ "$operation" = refresh ]; then
  /bin/cp "$DAEM_TEST_CODEX_PAYLOAD" "$CODEX_HOME/config.toml"
else
  /bin/cp "$DAEM_TEST_CODEX_PAYLOAD" "$CODEX_HOME/config.toml.next"
  /bin/chmod 600 "$CODEX_HOME/config.toml.next"
  /bin/mv "$CODEX_HOME/config.toml.next" "$CODEX_HOME/config.toml"
fi
if [ "$operation" = add ]; then
  mkdir -p "$CODEX_HOME/plugins/cache/openai-primary-runtime/documents/1.0.0"
  printf 'installed\n' > "$CODEX_HOME/plugins/cache/openai-primary-runtime/documents/1.0.0/plugin.txt"
elif [ "$operation" = remove ]; then
  if [ "${DAEM_TEST_CODEX_KEEP_CACHE:-0}" != 1 ]; then rm -rf "$CODEX_HOME/plugins/cache/openai-primary-runtime/documents"; fi
fi
if [ "$DAEM_TEST_CODEX_PHASE" = native ]; then
  case "$DAEM_TEST_CODEX_FAULT" in
    key) /bin/cp "$DAEM_TEST_CODEX_BAD_KEY" "$DAEM_TEST_CODEX_KEY_FILE" ;;
    outside) printf '\n[unexpected]\nvalue = true\n' >> "$CODEX_HOME/config.toml" ;;
    mode|mode-command) /bin/chmod 666 "$DAEM_TEST_CODEX_KEY_FILE" ;;
    command) exit 23 ;;
    terminate) /bin/chmod 666 "$DAEM_TEST_CODEX_KEY_FILE"; kill -KILL "$DAEM_TEST_CODEX_PARENT_PID"; exit 0 ;;
  esac
  [ "$DAEM_TEST_CODEX_FAULT" != mode-command ] || exit 23
fi
printf '{}\n'
`
	testkit.WriteFile(t, bin, "codex", script)
	if err := os.Chmod(filepath.Join(bin, "codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	paths, err := daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"DAEM_TEST_CODEX_BEFORE": fixture.before, "DAEM_TEST_CODEX_CALLS": fixture.calls, "DAEM_TEST_CODEX_STATE": fixture.launchState, "DAEM_TEST_CODEX_REGISTRY": paths.OwnershipRegistryPath, "DAEM_TEST_CODEX_LAUNCH_REGISTRY": fixture.launchRegistry, "DAEM_TEST_CODEX_PAYLOAD": fixture.payload, "DAEM_TEST_CODEX_KEY_FILE": fixture.keyFile, "DAEM_TEST_CODEX_KEY_BEFORE": filepath.Join(root, "key-before.toml"), "DAEM_TEST_CODEX_PHASE": "baseline", "DAEM_TEST_CODEX_FAULT": fault} {
		t.Setenv(name, value)
	}
	fixture.require(t, "lock")
	if previouslyInstalled {
		fixture.require(t, "apply", "--yes")
		testkit.AssertFileContent(t, fixture.calls, "add\n")
	}
	testkit.WriteFile(t, home, "config.toml", fixture.withoutPlugin)
	if err := os.Chmod(fixture.config, 0o644); err != nil {
		t.Fatal(err)
	}
	testkit.WriteFile(t, workspace, "preferences.toml", "model='native-model'\n")
	declaration := string(testkit.ReadFile(t, fixture.manifest)) + fmt.Sprintf("\n[[host_config]]\nid='preferences'\nscope=%q\npath='config.toml'\nsource=%q\nownership='keys'\n", scope, filepath.Join(workspace, "preferences.toml"))
	testkit.WriteFile(t, workspace, "daem.toml", declaration)
	if scope == target.ScopeGlobal {
		fixture.withoutPlugin = "model = 'native-model'\n" + fixture.withoutPlugin
		fixture.withPlugin = "model = 'native-model'\n" + fixture.withPlugin
	}
	testkit.WriteFile(t, root, "payload.toml", fixture.withPlugin)
	badKey := "model='changed'\n"
	if scope == target.ScopeGlobal {
		badKey = strings.Replace(fixture.withPlugin, "model = 'native-model'", "model = 'changed'", 1)
	}
	testkit.WriteFile(t, root, "bad-key.toml", badKey)
	t.Setenv("DAEM_TEST_CODEX_BAD_KEY", filepath.Join(root, "bad-key.toml"))
	t.Setenv("DAEM_TEST_CODEX_PHASE", "native")
	fixture.require(t, "lock")
	return fixture
}

func (fixture nativeCodexFixture) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	args = append(args, "--manifest", fixture.manifest)
	code := testkit.RunVerboseCLIWithOptions(args, clipkg.RunOptions{Stdout: &stdout, Stderr: &stderr})
	return code, stdout.String(), stderr.String()
}

func (fixture nativeCodexFixture) require(t *testing.T, args ...string) {
	t.Helper()
	if code, stdout, stderr := fixture.run(args...); code != 0 {
		t.Fatalf("actual Codex CLI %q failed: %d %s %s", args, code, stdout, stderr)
	}
}

func TestNativeCodexPluginEstablishesKeysBeforeProtectedAdd(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativeCodexFixture(t, scope, "")
			fixture.require(t, "apply", "--yes", "--json")
			if !strings.Contains(string(testkit.ReadFile(t, os.Getenv("DAEM_TEST_CODEX_KEY_BEFORE"))), "native-model") {
				t.Fatal("Codex ran before the selected key was established")
			}
			snapshot, err := statefile.Decode(testkit.ReadFile(t, fixture.launchState))
			if err != nil || len(snapshot.PendingCarrierInstalls()) != 1 {
				t.Fatal("Codex add lacks exact install correlation", err)
			}
			store, err := ownershipstore.New(fixture.launchRegistry)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
				t.Fatal("Codex add was released without Bound protection", err)
			}
			info, err := os.Stat(fixture.config)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatal("legitimate Codex replacement did not retain its tightened mode", err)
			}
			fixture.require(t, "status", "--check", "--json")
			fixture.require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.calls, "add\nadd\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativeCodexPluginUsesCommandRootForRelativeHome(t *testing.T) {
	fixture := newNativeCodexFixture(t, target.ScopeGlobal, "")
	relative, err := filepath.Rel(fixture.workspace, filepath.Dir(fixture.config))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", relative)
	fixture.require(t, "apply", "--yes", "--json")
	fixture.require(t, "status", "--check", "--json")
	fixture.require(t, "remove", "extension", "documents")
	fixture.require(t, "lock")
	testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), fixture.withoutPlugin)
	fixture.require(t, "apply", "--yes", "--json")
	testkit.AssertFileContent(t, fixture.calls, "add\nadd\nremove\n")
}

func TestNativeCodexFirstPluginInstallWithKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativeCodexFixtureWithHistory(t, scope, "", false)
			fixture.require(t, "apply", "--yes", "--json")
			fixture.require(t, "status", "--check", "--json")
			fixture.require(t, "apply", "--yes", "--json")
			testkit.AssertFileContent(t, fixture.calls, "add\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativeCodexPluginLossRecoversWithoutAddReplay(t *testing.T) {
	fixture := newNativeCodexFixture(t, target.ScopeGlobal, "terminate")
	code, stdout, stderr := nativefixture.InvokeRecoveryCommand(t, nativefixture.RecoveryFixture{Manifest: fixture.manifest, Destination: fixture.keyFile}, "native-codex-command", "apply", "--yes", "--json")
	if code == 0 || code == nativefixture.RecoveryExit {
		t.Fatalf("Codex Apply was not interrupted: %d %s %s", code, stdout, stderr)
	}
	store, err := ownershipstore.New(os.Getenv("DAEM_TEST_CODEX_REGISTRY"))
	if err != nil {
		t.Fatal(err)
	}
	registry, err := store.LoadProtectionRegistry(t.Context())
	if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
		t.Fatal("lost Codex invocation has no durable Bound protection", err)
	}
	if err := os.Remove(filepath.Join(fixture.workspace, "preferences.toml")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DAEM_TEST_CODEX_FAULT", "")
	otherRoot := filepath.Join(filepath.Dir(fixture.workspace), "other-workspace")
	other := nativeCodexFixture{workspace: otherRoot, manifest: filepath.Join(otherRoot, "daem.toml")}
	testkit.WriteFile(t, otherRoot, "daem.toml", "version=1\ntargets=['codex']\n[[extension]]\nid='documents'\ncarrier='codex-plugin'\nscope='global'\nsource={marketplace='documents@openai-primary-runtime'}\n")
	other.require(t, "lock")
	if code, _, _ := other.run("refresh", "extension", "documents", "--yes", "--json"); code == 0 {
		t.Fatal("ordinary Codex Refresh bypassed another owner's pending protection")
	}
	fixture.require(t, "recover", "--dry-run")
	fixture.require(t, "recover", "--yes")
	info, err := os.Stat(fixture.keyFile)
	if err != nil || info.Mode().Perm()&0o022 != 0 {
		t.Fatal("Codex recovery left broadened write access", err)
	}
	testkit.WriteFile(t, fixture.workspace, "preferences.toml", "model='native-model'\n")
	fixture.require(t, "apply", "--yes")
	fixture.require(t, "status", "--check", "--json")
	other.require(t, "refresh", "extension", "documents", "--dry-run", "--json")
	testkit.AssertFileContent(t, fixture.calls, "add\nadd\n")
	testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
}

func TestNativeCodexPluginRejectsChangesAndContainsPermissions(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, fault := range []string{"key", "outside", "mode", "command", "mode-command", "comment", "sibling"} {
			t.Run(string(scope)+"/"+fault, func(t *testing.T) {
				fixture := newNativeCodexFixture(t, scope, fault)
				if fault == "comment" {
					testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), strings.Replace(fixture.withPlugin, "# retained root note\n", "", 1))
				}
				if fault == "sibling" {
					testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), strings.Replace(fixture.withPlugin, "enabled = true", "enabled = false", 1))
				}
				code, stdout, stderr := fixture.run("apply", "--yes", "--json")
				// A project-only key does not own the separate global config file.
				wantFailure := scope == target.ScopeGlobal || fault == "key" || fault == "mode" || fault == "command" || fault == "mode-command"
				if wantFailure != (code != 0) {
					t.Fatalf("unexpected qualified outcome: %d %s %s", code, stdout, stderr)
				}
				var result struct {
					Status    string `json:"status"`
					HasErrors bool   `json:"has_errors"`
				}
				if err := json.Unmarshal([]byte(stdout), &result); err != nil {
					t.Fatal(err)
				}
				if wantFailure && !result.HasErrors {
					t.Fatal("changed settings or command failure became success")
				}
				info, err := os.Stat(fixture.keyFile)
				if err != nil || info.Mode().Perm()&0o022 != 0 {
					t.Fatal("Codex invocation left broadened write access", err)
				}
				if fault == "key" && !strings.Contains(string(testkit.ReadFile(t, fixture.keyFile)), "changed") {
					t.Fatal("selected key was automatically repaired")
				}
				testkit.AssertFileContent(t, fixture.calls, "add\nadd\n")
				store, err := ownershipstore.New(os.Getenv("DAEM_TEST_CODEX_REGISTRY"))
				if err != nil {
					t.Fatal(err)
				}
				registry, err := store.LoadProtectionRegistry(t.Context())
				if err != nil || len(registry.PendingProtections()) != 0 {
					t.Fatal("qualified writer left pending safety work", err)
				}
			})
		}
	}
}

func TestNativeCodexPluginRemovalRetainsOrRetiresManagedKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, retire := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/retire=%t", scope, retire), func(t *testing.T) {
				fixture := newNativeCodexFixture(t, scope, "")
				fixture.require(t, "apply", "--yes")
				fixture.require(t, "remove", "extension", "documents")
				if retire {
					testkit.WriteFile(t, fixture.workspace, "daem.toml", "version=1\ntargets=['codex']\n")
				}
				fixture.require(t, "lock")
				testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), fixture.withoutPlugin)
				fixture.require(t, "apply", "--yes", "--json")
				snapshot, err := statefile.Decode(testkit.ReadFile(t, fixture.launchState))
				if err != nil || len(snapshot.PendingCarrierRemovals()) != 1 {
					t.Fatal("Codex removal lacks durable correlation", err)
				}
				paths, err := daempaths.Resolve(fixture.manifest)
				if err != nil {
					t.Fatal(err)
				}
				after, err := statefile.Decode(testkit.ReadFile(t, paths.StatefilePath))
				if err != nil || after.ContainsHostConfig() == retire {
					t.Fatal("native key settlement was lost during removal", err)
				}
				if _, err := os.Stat(filepath.Join(os.Getenv("CODEX_HOME"), "plugins/cache/openai-primary-runtime/documents")); !os.IsNotExist(err) {
					t.Fatal("selected Codex cache was not removed", err)
				}
				fixture.require(t, "status", "--check", "--json")
				fixture.require(t, "apply", "--yes")
				testkit.AssertFileContent(t, fixture.calls, "add\nadd\nremove\n")
				testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
			})
		}
	}
}

func TestNativeCodexMarketplaceRefreshUsesProtectedCurrentKeys(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			fixture := newNativeCodexFixture(t, scope, "")
			fixture.require(t, "apply", "--yes")
			testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), fixture.withPlugin+"[marketplaces.openai-primary-runtime]\nsource='/updated-marketplace'\n")
			fixture.require(t, "refresh", "extension", "documents", "--dry-run", "--json")
			fixture.require(t, "refresh", "extension", "documents", "--yes", "--json")
			store, err := ownershipstore.New(fixture.launchRegistry)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := store.LoadProtectionRegistry(t.Context())
			if err != nil || len(registry.PendingProtections()) != 1 || registry.PendingProtections()[0].Phase() != ownership.ProtectionBound {
				t.Fatal("marketplace refresh was not protected at launch", err)
			}
			fixture.require(t, "status", "--check", "--json")
			testkit.AssertFileContent(t, fixture.calls, "add\nadd\nrefresh\n")
			testkit.AssertNoRecoveryArtifacts(t, fixture.workspace)
		})
	}
}

func TestNativeCodexRemovalRequiresCacheAbsenceBeforeSettlement(t *testing.T) {
	fixture := newNativeCodexFixture(t, target.ScopeGlobal, "")
	fixture.require(t, "apply", "--yes")
	fixture.require(t, "remove", "extension", "documents")
	fixture.require(t, "lock")
	testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), fixture.withoutPlugin)
	t.Setenv("DAEM_TEST_CODEX_KEEP_CACHE", "1")
	if code, _, _ := fixture.run("apply", "--yes", "--json"); code == 0 {
		t.Fatal("config-only absence retired a Codex removal with its cache retained")
	}
	paths, err := daempaths.Resolve(fixture.manifest)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := statefile.Decode(testkit.ReadFile(t, paths.StatefilePath))
	if err != nil || len(snapshot.PendingCarrierRemovals()) != 1 {
		t.Fatal("failed cache postcondition lost retryable removal", err)
	}
	t.Setenv("DAEM_TEST_CODEX_KEEP_CACHE", "0")
	if code, _, _ := fixture.run("apply", "--yes", "--json"); code == 0 {
		t.Fatal("pending cache postcondition was silently dropped on retry")
	}
	if err := os.RemoveAll(filepath.Join(os.Getenv("CODEX_HOME"), "plugins/cache/openai-primary-runtime/documents")); err != nil {
		t.Fatal(err)
	}
	fixture.require(t, "apply", "--yes", "--json")
	fixture.require(t, "status", "--check", "--json")
	testkit.AssertFileContent(t, fixture.calls, "add\nadd\nremove\n")
}

func TestNativeCodexMarketplaceRefreshRejectsPluginAndKeyChanges(t *testing.T) {
	for _, fault := range []string{"key", "outside", "mode", "command", "mode-command", "comment", "sibling"} {
		t.Run(fault, func(t *testing.T) {
			fixture := newNativeCodexFixture(t, target.ScopeGlobal, "")
			fixture.require(t, "apply", "--yes")
			t.Setenv("DAEM_TEST_CODEX_FAULT", fault)
			payload := fixture.withPlugin
			if fault == "comment" {
				payload = strings.Replace(payload, "# retained root note\n", "", 1)
			}
			if fault == "sibling" {
				payload = strings.Replace(payload, "enabled = true", "enabled = false", 1)
			}
			testkit.WriteFile(t, filepath.Dir(fixture.payload), filepath.Base(fixture.payload), payload)
			code, stdout, stderr := fixture.run("refresh", "extension", "documents", "--yes", "--json")
			if code == 0 {
				t.Fatalf("unqualified marketplace refresh succeeded: %s %s", stdout, stderr)
			}
			var result struct {
				Result struct {
					Class string `json:"class"`
				} `json:"result"`
			}
			wantClass := "partial"
			if fault == "command" {
				wantClass = "failed"
			}
			if err := json.Unmarshal([]byte(stdout), &result); err != nil || result.Result.Class != wantClass {
				t.Fatal("refresh lost its started partial outcome", err, stdout)
			}
			info, err := os.Stat(fixture.keyFile)
			if err != nil || info.Mode().Perm()&0o022 != 0 {
				t.Fatal("refresh left expanded access", err)
			}
			testkit.AssertFileContent(t, fixture.calls, "add\nadd\nrefresh\n")
		})
	}
}
