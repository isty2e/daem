package codexplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	observeconfig "github.com/isty2e/daem/internal/assurance/observe/config"
)

func TestObserveConfigFileReportsMissingConfigWithoutError(t *testing.T) {
	observation, err := ObserveConfigFile(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	if observation.ConfigExists() {
		t.Fatalf("ConfigExists = true, want false")
	}
	if len(observation.Entries()) != 0 {
		t.Fatalf("Entries = %#v, want empty", observation.Entries())
	}
}

func TestObserveConfigFileReportsPermissionDeniedAsReadError(t *testing.T) {
	observation, err := observeConfigFile(
		filepath.Join(t.TempDir(), "config.toml"),
		func(string) ([]byte, error) {
			return nil, os.ErrPermission
		},
	)
	if err == nil {
		t.Fatalf("observeConfigFile returned nil error, want permission error")
	}
	if observation.ConfigExists() {
		t.Fatalf("ConfigExists = true, want false when read permission blocks observation")
	}
	if observation.EntrySetObserved() || len(observation.Entries()) != 0 {
		t.Fatalf("observation = %#v, want no observed entries after permission error", observation)
	}
}

func TestObserveConfigFileReportsPluginActivationDisclosure(t *testing.T) {
	configPath := writeCodexConfig(t, `
[plugins."alpha@market"]
enabled = true

[plugins."beta@market"]
enabled = false

[plugins."gamma@market"]
`)

	observation, err := ObserveConfigFile(configPath)
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	if !observation.ConfigExists() {
		t.Fatalf("ConfigExists = false, want true")
	}
	want := []entryWant{
		{key: "alpha@market", activation: observeconfig.ActivationConfiguredTrue},
		{key: "beta@market", activation: observeconfig.ActivationConfiguredFalse},
		{key: "gamma@market", activation: observeconfig.ActivationNotDeclared},
	}
	entries := observation.Entries()
	if len(entries) != len(want) {
		t.Fatalf("Entries = %#v, want %#v", entries, want)
	}
	for index := range want {
		assertConfigEntry(t, entries[index], want[index])
	}
}

func TestObserveConfigFileKeepsSameVisiblePluginNamesSeparate(t *testing.T) {
	configPath := writeCodexConfig(t, `
[plugins."same@alpha"]
enabled = true

[plugins."same@beta"]
enabled = true
`)

	observation, err := ObserveConfigFile(configPath)
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	entries := observation.Entries()
	keys := []string{string(entries[0].Key()), string(entries[1].Key())}
	want := []string{"same@alpha", "same@beta"}
	for index := range want {
		if keys[index] != want[index] {
			t.Fatalf("keys = %#v, want %#v", keys, want)
		}
	}
}

func TestObserveConfigFileHandlesDottedPluginTableSyntax(t *testing.T) {
	configPath := writeCodexConfig(t, `
	plugins."alpha@market".enabled = true
	`)

	observation, err := ObserveConfigFile(configPath)
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	entries := observation.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries = %#v, want one entry", entries)
	}
	entry := entries[0]
	if entry.Key() != observeconfig.Key("alpha@market") ||
		entry.Activation() != observeconfig.ActivationConfiguredTrue ||
		!entry.Observed() {
		t.Fatalf("entry = %#v, want dotted plugin table observation", entry)
	}
}

func TestObserveConfigFileHandlesCRLFDottedPluginTableSyntax(t *testing.T) {
	configPath := writeCodexConfig(t, "plugins.\"alpha@market\".enabled = true\r\n")

	observation, err := ObserveConfigFile(configPath)
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	entries := observation.Entries()
	if len(entries) != 1 || entries[0].Key() != "alpha@market" ||
		entries[0].Activation() != observeconfig.ActivationConfiguredTrue {
		t.Fatalf("Entries = %#v, want one CRLF dotted plugin observation", entries)
	}
}

func TestObserveConfigFileReportsUnsupportedPluginShapes(t *testing.T) {
	t.Run("plugins not table", func(t *testing.T) {
		configPath := writeCodexConfig(t, `plugins = "not a table"`)

		observation, err := ObserveConfigFile(configPath)
		if err != nil {
			t.Fatalf("ObserveConfigFile returned error: %v", err)
		}
		if !observation.EntrySetUnsupported() {
			t.Fatalf("EntrySetUnsupported = false, want true")
		}
	})

	t.Run("entry not table", func(t *testing.T) {
		configPath := writeCodexConfig(t, `plugins."alpha@market" = true`)

		observation, err := ObserveConfigFile(configPath)
		if err != nil {
			t.Fatalf("ObserveConfigFile returned error: %v", err)
		}
		entry := observation.Entries()[0]
		if !entry.Unsupported() || entry.Reason() != observeconfig.ReasonEntryNotTable {
			t.Fatalf("entry = %#v, want entry_not_table", entry)
		}
	})

	t.Run("empty config key", func(t *testing.T) {
		configPath := writeCodexConfig(t, `
[plugins.""]
enabled = true
`)

		observation, err := ObserveConfigFile(configPath)
		if err != nil {
			t.Fatalf("ObserveConfigFile returned error: %v", err)
		}
		entry := observation.Entries()[0]
		if !entry.Unsupported() || entry.Reason() != observeconfig.ReasonEmptyEntryKey {
			t.Fatalf("entry = %#v, want empty_config_key", entry)
		}
	})
}

func TestObserveConfigFileDoesNotDecodeUnrelatedSecretOrMarketplaceValues(t *testing.T) {
	configPath := writeCodexConfig(t, `
[plugins."alpha@market"]
enabled = true

[marketplaces.private]
source = "https://token@example.invalid/repo.git"

[credentials]
api_key = "secret-value"
`)

	observation, err := ObserveConfigFile(configPath)
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	entries := observation.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries = %#v, want only plugin entries", entries)
	}
	if entries[0].Key() != observeconfig.Key("alpha@market") {
		t.Fatalf("EntryKey = %q", entries[0].Key())
	}
}

func TestObserveConfigFileReportsNonBooleanActivationAsUnsupportedEntry(t *testing.T) {
	configPath := writeCodexConfig(t, `
[plugins."alpha@market"]
enabled = "yes"
`)

	observation, err := ObserveConfigFile(configPath)
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	entries := observation.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries = %#v, want one entry", entries)
	}
	entry := entries[0]
	if !entry.Unsupported() ||
		entry.Activation() != observeconfig.ActivationUnsupportedType ||
		entry.Reason() != observeconfig.ReasonActivationNotBoolean {
		t.Fatalf("entry = %#v, want unsupported activation disclosure", entry)
	}
}

func TestObserveConfigFileReportsMalformedTOML(t *testing.T) {
	configPath := writeCodexConfig(t, "[plugins.\"alpha@market\"\n")

	observation, err := ObserveConfigFile(configPath)
	if err == nil {
		t.Fatalf("ObserveConfigFile returned nil error, want malformed TOML error")
	}
	if !observation.ConfigExists() {
		t.Fatalf("ConfigExists = false, want true for malformed existing config")
	}
}

func TestObserveConfigFileReportsDuplicatePluginTablesAsMalformedTOML(t *testing.T) {
	configPath := writeCodexConfig(t, `
[plugins."alpha@market"]
enabled = true

[plugins."alpha@market"]
enabled = false
`)

	if _, err := ObserveConfigFile(configPath); err == nil {
		t.Fatalf("ObserveConfigFile returned nil error, want duplicate table TOML error")
	}
}

func TestExactConfiguredSourcesRejectsUnsupportedAndMalformedSelectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "unsupported row",
			content: `[plugins."bad@market"]
enabled = "yes"
`,
			want: "unsupported",
		},
		{
			name: "malformed selector",
			content: `[plugins.bad]
enabled = true
`,
			want: "PLUGIN@MARKETPLACE",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			observation, err := ObserveConfigFile(writeCodexConfig(t, test.content))
			if err != nil {
				t.Fatalf("ObserveConfigFile: %v", err)
			}
			if _, err := ExactConfiguredSources(observation); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("ExactConfiguredSources error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestObserveConfigFileRejectsSymlinkedAuthorityFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	targetPath := filepath.Join(root, "target.toml")
	if err := os.WriteFile(targetPath, []byte(`[plugins."one@market"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "config.toml")
	if err := os.Symlink(targetPath, configPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := ObserveConfigFile(configPath); err == nil ||
		!strings.Contains(err.Error(), "symlink") {
		t.Fatalf("ObserveConfigFile error = %v", err)
	}
}

func TestObserveConfigFileBlocksPluginKeyOverflow(t *testing.T) {
	observation, err := ObserveConfigFile(writeCodexConfig(t, overflowingCodexPluginConfig()))
	if err != nil {
		t.Fatalf("ObserveConfigFile returned error: %v", err)
	}
	if !observation.ConfigExists() ||
		!observation.EntrySetBudgetExceeded() ||
		observation.EntrySetObserved() ||
		len(observation.Entries()) != 0 {
		t.Fatalf("observation = %#v, want budget-exceeded entry set without stored entries", observation)
	}
	if _, err := ExactConfiguredSources(observation); err == nil ||
		!strings.Contains(err.Error(), "budget") {
		t.Fatalf("ExactConfiguredSources error = %v, want budget rejection", err)
	}
}

func overflowingCodexPluginConfig() string {
	var body strings.Builder
	for index := 0; index < MaximumObservationEntries+1; index++ {
		fmt.Fprintf(&body, "[plugins.%q]\nenabled = true\n", versionName(index)+"@market")
	}
	return body.String()
}

func writeCodexConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}

type entryWant struct {
	key        observeconfig.Key
	activation observeconfig.ActivationDisclosure
	reason     observeconfig.ReasonCode
}

func assertConfigEntry(t *testing.T, entry observeconfig.Entry, want entryWant) {
	t.Helper()
	if entry.Key() != want.key ||
		!entry.Observed() ||
		entry.Activation() != want.activation ||
		entry.Reason() != want.reason {
		t.Fatalf("entry = %#v, want %#v", entry, want)
	}
}
