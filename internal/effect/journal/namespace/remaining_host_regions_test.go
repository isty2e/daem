package namespace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func TestClaudeAndAntigravityStoresRejectForeignAliasesWithoutBanningConfiguration(t *testing.T) {
	for _, row := range []struct {
		path     string
		host     target.Target
		reserved bool
	}{
		{"claude/.credentials.json", target.TargetClaudeCode, true},
		{"secure/.credentials.json", target.TargetClaudeCode, true},
		{"claude/projects/project/session.jsonl", target.TargetClaudeCode, true},
		{"claude/plugins/cache/market/plugin/version/plugin.json", target.TargetClaudeCode, true},
		{"claude/settings.json", target.TargetClaudeCode, false},
		{"claude/daemon.json", target.TargetClaudeCode, false},
		{"claude/agents/custom.json", target.TargetClaudeCode, false},
		{"home/.gemini/jetski-standalone-oauth-token", target.TargetAntigravityCLI, true},
		{"home/.gemini/cache/antigravity-keyring-unavailable", target.TargetAntigravityCLI, true},
		{"home/.gemini/config/plugins/plugin/plugin.json", target.TargetAntigravityCLI, true},
		{"home/.gemini/config/import_manifest.json", target.TargetAntigravityCLI, true},
		{"home/.gemini/antigravity-cli/brain/conversation/state.json", target.TargetAntigravityCLI, true},
		{"home/.gemini/antigravity-cli/mcp/server/tool.json", target.TargetAntigravityCLI, true},
		{"home/.gemini/antigravity-cli/secplugins/grants/grant.json", target.TargetAntigravityCLI, true},
		{"home/.gemini/antigravity-cli/secplugins/config.json", target.TargetAntigravityCLI, false},
		{"home/.gemini/antigravity-cli/settings.json", target.TargetAntigravityCLI, false},
		{"home/.gemini/antigravity-cli/keybindings.json", target.TargetAntigravityCLI, false},
		{"home/.gemini/antigravity-cli/commands/custom.json", target.TargetAntigravityCLI, false},
	} {
		t.Run(row.path, func(t *testing.T) {
			base := t.TempDir()
			selected := filepath.Join(base, filepath.FromSlash(row.path))
			if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
				t.Fatal(err)
			}
			protection := capturedProtection(t, base, filepath.Join(base, "home"), map[string]string{
				"CLAUDE_CONFIG_DIR": filepath.Join(base, "claude"), "CLAUDE_SECURESTORAGE_CONFIG_DIR": filepath.Join(base, "secure"),
			})
			observed, reader, err := observePrivateTarget(t, base, selected, protection)
			if row.reserved {
				var failure *ProtectionError
				if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != row.host || reader.imageReads != 0 {
					t.Fatal("private host storage reached a foreign native document", err, reader.imageReads)
				}
				return
			}
			if err != nil {
				t.Fatal("ordinary configuration was confused with private application storage", err)
			}
			if image, _, _, err := observed.ReadReferent(t.Context(), 1024, imageBudget(t)); err != nil || string(image.Content()) != `{"fixture":true}` {
				t.Fatal("ordinary configuration could not retain its contents", err)
			}
		})
	}
}

func TestRepeatedPluginDirectoryAliasesChargeTheirExpansionWork(t *testing.T) {
	base := t.TempDir()
	store := filepath.Join(base, "plugin-store")
	cache := filepath.Join(store, "cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"first", "second"} {
		if err := os.Symlink(cache, filepath.Join(cache, name)); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
	}
	if err := os.Mkdir(filepath.Join(base, ".pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, ".pi", "shared.json"), []byte(`{"fixture":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	protection := capturedProtection(t, base, filepath.Join(base, "home"), map[string]string{"CLAUDE_CODE_PLUGIN_CACHE_DIR": store})
	roots := fixtureNativeRoots(t, map[output.RootRole]RootSelection{output.RootProject: nativeNamespaceRoot(t, base)})
	address := nativeNamespaceAddress(t, target.TargetPi, target.ScopeProject, ".pi/shared.json")
	quota := errors.New("expanded-entry allowance exhausted")
	budget := &pluginExpansionBudget{Budget: imageBudget(t), allowance: 8, refusal: quota}
	reader := &nativeNamespaceReader{RootedReader: journalTestFilesystem()}
	value, err := Observe(t.Context(), []aggregate.DocumentAddress{address}, roots, protection, reader, budget)
	var failure *ProtectionError
	if !errors.Is(err, quota) || !errors.As(err, &failure) || failure.Uncertainty() != ProtectionWorkLimit || value.Observation().Validate() == nil || reader.imageReads != 0 {
		t.Fatal("cached directory aliases bypassed physical-work admission", err, budget.allowance)
	}
}

func TestConfiguredPluginStoreOwnsItsEnvelopeEvenAtASharedAnchor(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	selected := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(selected), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	protection := capturedProtection(t, base, home, map[string]string{"CLAUDE_CODE_PLUGIN_CACHE_DIR": "~"})
	_, reader, err := observePrivateTarget(t, base, selected, protection)
	var failure *ProtectionError
	if !errors.As(err, &failure) || failure.Kind() != ProtectionReserved || failure.Host() != target.TargetClaudeCode || reader.imageReads != 0 {
		t.Fatal("the configured plugin envelope was weakened for an overlapping settings location", err, reader.imageReads)
	}
}

type pluginExpansionBudget struct {
	*physicalwork.Budget
	allowance int
	refusal   error
}

func (budget *pluginExpansionBudget) AdmitPhysicalWork(components, entries int, bytes int64) error {
	if entries > budget.allowance {
		return budget.refusal
	}
	budget.allowance -= entries
	return budget.Budget.AdmitPhysicalWork(components, entries, bytes)
}

func TestPluginVersionLinksUseTheSameBoundedDirectoryPatterns(t *testing.T) {
	for _, override := range []bool{false, true} {
		base := t.TempDir()
		values := map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join(base, "claude")}
		store := filepath.Join(base, "claude", "plugins")
		if override {
			store = filepath.Join(base, "plugin-store")
			values["CLAUDE_CODE_PLUGIN_CACHE_DIR"] = store
		}
		parent := filepath.Join(store, "cache", "market", "plugin")
		if err := os.MkdirAll(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		checkout := filepath.Join(base, "checkout")
		if err := os.Mkdir(checkout, 0o700); err != nil {
			t.Fatal(err)
		}
		selected := filepath.Join(checkout, "plugin.json")
		if err := os.WriteFile(selected, []byte(`{"fixture":true}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(checkout, filepath.Join(parent, "version")); err != nil {
			t.Skipf("symlinks are unavailable: %v", err)
		}
		_, reader, err := observePrivateTarget(t, base, selected, capturedProtection(t, base, filepath.Join(base, "home"), values))
		var failure *ProtectionError
		if !errors.Is(err, configuration.ErrReserved) || !errors.As(err, &failure) || failure.Host() != target.TargetClaudeCode || reader.imageReads != 0 {
			t.Error("a version-directory link escaped installed-code protection", override, err, reader.imageReads)
		}
	}
}
