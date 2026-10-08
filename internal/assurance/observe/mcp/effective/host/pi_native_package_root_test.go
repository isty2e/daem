package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestPiNativePackageRootPrecedenceAndQueryProvenance(t *testing.T) {
	for _, test := range []struct {
		name, manager, queryLog string
		scope                   target.Scope
		managed                 bool
		pnpmMatch               bool
		legacyExists            bool
	}{
		{"managed user preempts legacy", "npm", "", target.ScopeGlobal, true, false, true},
		{"project never queries legacy", "npm", "", target.ScopeProject, false, false, true},
		{"npm user fallback", "npm", "root -g\n", target.ScopeGlobal, false, false, true},
		{"pnpm package-specific root", "pnpm", "list -g --depth 0 --json\n", target.ScopeGlobal, false, true, true},
		{"pnpm no-match generic fallback", "pnpm", "list -g --depth 0 --json\nroot -g\n", target.ScopeGlobal, false, false, true},
		{"pnpm absent package-specific root no retry", "pnpm", "list -g --depth 0 --json\n", target.ScopeGlobal, false, true, false},
		{"Bun derived global root", "bun", "pm bin -g\n", target.ScopeGlobal, false, false, true},
		{"npm absent legacy returns managed candidate", "npm", "root -g\n", target.ScopeGlobal, false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir, agentRoot := t.TempDir(), t.TempDir()
			root := filepath.Join(workDir, "legacy", "node_modules")
			legacy := filepath.Join(root, "pi-mcp-adapter")
			listing := "[]"
			if test.pnpmMatch {
				legacy = filepath.Join(workDir, "pnpm-store", "adapter")
				pathJSON, err := json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
				listing = `[{"dependencies":{"pi-mcp-adapter":{"path":` + string(pathJSON) + `}}}]`
			} else if test.manager == "bun" {
				root = filepath.Join(workDir, "legacy", "bin")
				legacy = filepath.Join(workDir, "legacy", "install", "global", "node_modules", "pi-mcp-adapter")
			}
			if test.legacyExists {
				if err := os.MkdirAll(legacy, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			managed := filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter")
			if test.scope == target.ScopeProject {
				managed = filepath.Join(workDir, ".pi", "npm", "node_modules", "pi-mcp-adapter")
			}
			if test.managed {
				writeEffectiveConfig(t, filepath.Join(managed, "package.json"), "malformed")
			}
			command, log := installNativeRootQueryFixture(t, test.manager, root, listing)
			settings := piNativePackageContext{workDir: workDir, agentRoot: agentRoot, npmCommand: json.RawMessage(`["` + command + `"]`)}

			selected, err := nativeAdapterPackageRoot(t.Context(), test.scope, settings)
			wanted := managed
			if test.scope == target.ScopeGlobal && !test.managed && test.legacyExists {
				wanted = legacy
			}
			if err != nil || selected != wanted {
				t.Fatalf("selected package root = %q, %v; want %q", selected, err, wanted)
			}
			assertNativeRootQueryLog(t, log, test.queryLog)
		})
	}
}

func TestPiNativePackageRootRefusesQueryFailuresWithoutRetry(t *testing.T) {
	for _, test := range []struct{ name, manager, root, listing, fail, queryLog string }{
		{"malformed pnpm listing", "pnpm", "/available", "{", "", "list -g --depth 0 --json\n"},
		{"pnpm listing failure", "pnpm", "/available", "[]", "1", "list -g --depth 0 --json\n"},
		{"non-array pnpm listing", "pnpm", "/available", "{}", "", "list -g --depth 0 --json\n"},
		{"wrong pnpm dependency shape", "pnpm", "/available", `[{"dependencies":[]}]`, "", "list -g --depth 0 --json\n"},
		{"relative npm root", "npm", "relative", "", "", "root -g\n"},
		{"empty npm root", "npm", "", "", "", "root -g\n"},
		{"multiline npm root", "npm", "/first\n/second", "", "", "root -g\n"},
		{"relative pnpm package path", "pnpm", "/available", `[{"dependencies":{"pi-mcp-adapter":{"path":"relative"}}}]`, "", "list -g --depth 0 --json\n"},
		{"npm query failure", "npm", "/available", "", "1", "root -g\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			command, log := installNativeRootQueryFixture(t, test.manager, test.root, test.listing)
			t.Setenv("DAEM_NATIVE_ROOT_FAIL", test.fail)
			settings := piNativePackageContext{workDir: t.TempDir(), agentRoot: t.TempDir(), npmCommand: json.RawMessage(`["` + command + `"]`)}
			if root, err := nativeAdapterPackageRoot(t.Context(), target.ScopeGlobal, settings); err == nil || root != "" {
				t.Fatalf("failed query supplied root evidence: %q, %v", root, err)
			}
			assertNativeRootQueryLog(t, log, test.queryLog)
		})
	}
}

func TestPiNativePackageLookupCommandAndCancellationBoundaries(t *testing.T) {
	for _, raw := range []string{`["npm","install"]`, `["wrapper","--","npm"]`, `["other"]`, `[""]`, `"npm"`, `["npm",null]`, `["./npm"]`, `["npm.exe.cmd"]`} {
		t.Run(raw, func(t *testing.T) {
			_, log := installNativeRootQueryFixture(t, "npm", "/available", "[]")
			settings := piNativePackageContext{workDir: t.TempDir(), agentRoot: t.TempDir(), npmCommand: json.RawMessage(raw)}
			if root, err := nativeAdapterPackageRoot(t.Context(), target.ScopeGlobal, settings); err == nil || root != "" {
				t.Fatalf("unadmitted command supplied root evidence: %q, %v", root, err)
			}
			assertNativeRootQueryLog(t, log, "")
		})
	}
	t.Run("canceled before query", func(t *testing.T) {
		_, log := installNativeRootQueryFixture(t, "npm", "/available", "[]")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err := nativeAdapterPackageRoot(ctx, target.ScopeGlobal, piNativePackageContext{workDir: t.TempDir(), agentRoot: t.TempDir()})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation = %v", err)
		}
		assertNativeRootQueryLog(t, log, "")
	})
	t.Run("missing manager", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if root, err := nativeAdapterPackageRoot(t.Context(), target.ScopeGlobal, piNativePackageContext{workDir: t.TempDir(), agentRoot: t.TempDir()}); err == nil || root != "" {
			t.Fatalf("missing manager supplied root evidence: %q, %v", root, err)
		}
	})
	t.Run("truncated query output", func(t *testing.T) {
		command, _ := installNativeRootQueryFixture(t, "npm", "/available", "[]")
		t.Setenv("DAEM_NATIVE_ROOT_TRUNCATE", "1")
		if output, err := nativePackageLocationQuery(t.Context(), command, []string{"root", "-g"}, t.TempDir()); err == nil || output != "" {
			t.Fatal("truncated location query returned partial output")
		}
	})
}

func TestPiNativeDefaultAndDirectLookupCommands(t *testing.T) {
	for _, test := range []struct{ name, manager, configured string }{
		{"default", "npm", ""},
		{"null default", "npm", "null"},
		{"empty default", "npm", "[]"},
		{"direct npm", "npm", `["npm"]`},
		{"native command suffix", "npm.cmd", `["npm.cmd"]`},
		{"native executable suffix", "npm.EXE", `["npm.EXE"]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			workDir := t.TempDir()
			base := filepath.Join(workDir, "legacy root", "node_modules")
			wanted := filepath.Join(base, "pi-mcp-adapter")
			if err := os.MkdirAll(wanted, 0o700); err != nil {
				t.Fatal(err)
			}
			_, log := installNativeRootQueryFixture(t, test.manager, base, "[]")
			settings := piNativePackageContext{workDir: workDir, agentRoot: t.TempDir(), npmCommand: json.RawMessage(test.configured)}
			selected, err := nativeAdapterPackageRoot(t.Context(), target.ScopeGlobal, settings)
			if err != nil || selected != wanted {
				t.Fatalf("default/direct command selected %q, %v; want %q", selected, err, wanted)
			}
			assertNativeRootQueryLog(t, log, "root -g\n")
		})
	}
}

func TestPiNativeManagedPathExistenceSkipsLookup(t *testing.T) {
	for _, kind := range []string{"directory without metadata", "regular file"} {
		t.Run(kind, func(t *testing.T) {
			workDir, agentRoot := t.TempDir(), t.TempDir()
			managed := filepath.Join(agentRoot, "npm", "node_modules", "pi-mcp-adapter")
			if kind == "regular file" {
				writeEffectiveConfig(t, managed, "not a directory")
			} else if err := os.MkdirAll(managed, 0o700); err != nil {
				t.Fatal(err)
			}
			_, log := installNativeRootQueryFixture(t, "npm", "/unused", "[]")
			settings := piNativePackageContext{workDir: workDir, agentRoot: agentRoot, npmCommand: json.RawMessage(`["npm","install"]`)}
			selected, err := nativeAdapterPackageRoot(t.Context(), target.ScopeGlobal, settings)
			if err != nil || selected != managed {
				t.Fatalf("managed existence selected %q, %v; want %q", selected, err, managed)
			}
			assertNativeRootQueryLog(t, log, "")
		})
	}
}

func TestPiNativePackageLocationQueryUsesSelectedWorkDir(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprintf("available=%t", available), func(t *testing.T) {
			command, log := installNativeRootQueryFixture(t, "npm", "/available", "[]")
			workDir := t.TempDir()
			if !available {
				workDir = filepath.Join(workDir, "absent")
			}
			cwdLog := filepath.Join(t.TempDir(), "cwd.txt")
			t.Setenv("DAEM_NATIVE_ROOT_CWD_LOG", cwdLog)
			_, err := nativePackageLocationQuery(t.Context(), command, []string{"root", "-g"}, workDir)
			if !available {
				if err == nil {
					t.Fatal("missing operation directory did not refuse the query")
				}
				assertNativeRootQueryLog(t, log, "")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(cwdLog)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := os.Stat(strings.TrimSpace(string(content)))
			if err != nil {
				t.Fatal(err)
			}
			selected, err := os.Stat(workDir)
			if err != nil || !os.SameFile(observed, selected) {
				t.Fatalf("query cwd = %q, not selected %q: %v", content, workDir, err)
			}
			assertNativeRootQueryLog(t, log, "root -g\n")
		})
	}
}

func installNativeRootQueryFixture(t *testing.T, manager, root, listing string) (string, string) {
	t.Helper()
	bin := t.TempDir()
	command, log := filepath.Join(bin, manager), filepath.Join(bin, "calls.txt")
	t.Setenv("PATH", bin)
	t.Setenv("DAEM_NATIVE_ROOT_LOG", log)
	t.Setenv("DAEM_NATIVE_ROOT_RESPONSE", root)
	t.Setenv("DAEM_NATIVE_ROOT_LIST", listing)
	t.Setenv("DAEM_NATIVE_ROOT_FAIL", "")
	t.Setenv("DAEM_NATIVE_ROOT_TRUNCATE", "")
	t.Setenv("DAEM_NATIVE_ROOT_CWD_LOG", "")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DAEM_NATIVE_ROOT_LOG"
if [ -n "$DAEM_NATIVE_ROOT_CWD_LOG" ]; then pwd -P >> "$DAEM_NATIVE_ROOT_CWD_LOG"; fi
[ "$DAEM_NATIVE_ROOT_FAIL" != 1 ] || exit 97
if [ "$DAEM_NATIVE_ROOT_TRUNCATE" = 1 ]; then
  i=0
  while [ "$i" -lt 1025 ]; do printf '%4096s' ''; i=$((i + 1)); done
  exit 0
fi
case "$*" in
  'list -g --depth 0 --json') printf '%s\n' "$DAEM_NATIVE_ROOT_LIST" ;;
  'root -g'|'pm bin -g') printf '%s\n' "$DAEM_NATIVE_ROOT_RESPONSE" ;;
  *) exit 98 ;;
esac
`
	if err := os.WriteFile(command, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return command, log
}

func assertNativeRootQueryLog(t *testing.T, log, expected string) {
	t.Helper()
	content, err := os.ReadFile(log)
	if expected == "" && os.IsNotExist(err) {
		return
	}
	if err != nil || string(content) != expected {
		t.Fatalf("location query calls = %q, %v; want %q", strings.TrimSpace(string(content)), err, expected)
	}
}
