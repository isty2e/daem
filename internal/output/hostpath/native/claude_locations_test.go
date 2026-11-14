package native

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

func TestClaudeLocationConsumersKeepTheirOwnPresenceAndNormalizationRules(t *testing.T) {
	home, work := filepath.Join(t.TempDir(), "Cafe\u0301"), t.TempDir()
	contracts, err := configuration.NativeProtectionLocations()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name, reference string
		values          map[string]string
		want            string
		absent          bool
	}{
		{"empty configuration", "claude-preferences", map[string]string{"CLAUDE_CONFIG_DIR": ""}, work, false},
		{"empty account override", "claude-account", map[string]string{"CLAUDE_CONFIG_DIR": ""}, home, false},
		{"configuration normalization", "claude-preferences", map[string]string{"CLAUDE_CONFIG_DIR": "link/../Cafe\u0301"}, filepath.Join(work, "Café"), false},
		{"account spelling", "claude-account", map[string]string{"CLAUDE_CONFIG_DIR": "link/../Cafe\u0301"}, filepath.Join(work, "Cafe\u0301"), false},
		{"secure empty default", "claude-credentials", map[string]string{"CLAUDE_CONFIG_DIR": "other", "CLAUDE_SECURESTORAGE_CONFIG_DIR": ""}, filepath.Join(filepath.Dir(home), "Café", ".claude"), false},
		{"secure additional location absent", "claude-credentials", nil, "", true},
		{"plugin tilde", "claude-plugin-store", map[string]string{"CLAUDE_CODE_PLUGIN_CACHE_DIR": "~/linked/../plugins"}, filepath.Join(home, "plugins"), false},
		{"plugin override absent", "claude-plugin-store", map[string]string{"CLAUDE_CODE_PLUGIN_CACHE_DIR": ""}, "", true},
	} {
		t.Run(row.name, func(t *testing.T) {
			locations, err := NewLocations(work, home, row.values)
			if err != nil {
				t.Fatal(err)
			}
			for _, contract := range contracts {
				if contract.Reference().Identity() != row.reference {
					continue
				}
				location, err := locations.ResolveProtection(contract.Reference())
				if err != nil || location.Absent() != row.absent || location.Path() != row.want {
					t.Fatal("Claude consumers were collapsed into a different location protocol", location, err, row.want)
				}
				return
			}
			t.Fatal("location contract was not available for the lookup", row.reference)
		})
	}
}

func TestClaudeAccountFilesPreserveBothConsumerExpressions(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "Cafe\u0301")
	work := filepath.Join(base, "logical-cwd")
	for _, row := range []struct {
		name, configured string
		present          bool
		want             [2]string
	}{
		{"default", "", false, [2]string{filepath.Join(base, "Café", ".claude", ".config.json"), filepath.Join(home, ".claude.json")}},
		{"present empty", "", true, [2]string{filepath.Join(work, ".config.json"), filepath.Join(home, ".claude.json")}},
		{"relative normalized", "../Cafe\u0301", true, [2]string{work + string(filepath.Separator) + filepath.FromSlash("../Café/.config.json"), work + string(filepath.Separator) + filepath.FromSlash("../Cafe\u0301/.claude.json")}},
	} {
		t.Run(row.name, func(t *testing.T) {
			selected, err := ClaudeAccountFiles(row.configured, row.present, work, home)
			if err != nil || selected != row.want {
				t.Fatal("preferred/fallback account candidates lost their distinct consumer rules", selected, err, row.want)
			}
		})
	}
}
