package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestRejectCodexInlineHooksConflictIsScopedToCodexHookDestinations(t *testing.T) {
	err := ValidateAggregateReadPreconditions(".claude/settings.json", func(destination output.Destination) (string, error) {
		t.Fatalf("resolver called for non-Codex hook destination %q", destination)
		return "", nil
	})
	if err != nil {
		t.Fatalf("rejectCodexInlineHooksConflict returned error for non-Codex destination: %v", err)
	}
}

func TestRejectCodexInlineHooksConflictChecksPairedConfigOnly(t *testing.T) {
	tempDir := t.TempDir()

	cases := []struct {
		name              string
		hookDestination   output.Destination
		configDestination string
		configContent     string
		wantError         string
	}{
		{
			name:              "project inline hooks conflict",
			hookDestination:   ".codex/hooks.json",
			configDestination: ".codex/config.toml",
			configContent:     "[hooks]\n",
			wantError:         `unmanaged Codex inline hooks found in ".codex/config.toml"`,
		},
		{
			name:              "global inline hooks conflict",
			hookDestination:   "~/.codex/hooks.json",
			configDestination: "~/.codex/config.toml",
			configContent:     "hooks = []\n",
			wantError:         `unmanaged Codex inline hooks found in "~/.codex/config.toml"`,
		},
		{
			name:              "unrelated codex config table is allowed",
			hookDestination:   ".codex/hooks.json",
			configDestination: ".codex/config.toml",
			configContent:     "[mcp_servers.context7]\ncommand = \"npx\"\n",
		},
		{
			name:              "missing paired config is allowed",
			hookDestination:   ".codex/hooks.json",
			configDestination: ".codex/config.toml",
		},
		{
			name:              "malformed paired config remains boundary parse error",
			hookDestination:   ".codex/hooks.json",
			configDestination: ".codex/config.toml",
			configContent:     "[hooks\n",
			wantError:         `parse ".codex/config.toml" for Codex inline hooks`,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			caseDir := filepath.Join(tempDir, test.name)
			if test.configContent != "" {
				writeTestFile(t, caseDir, test.configDestination, test.configContent)
			}

			err := ValidateAggregateReadPreconditions(test.hookDestination, testDestinationResolver(caseDir))
			if test.wantError == "" {
				if err != nil {
					t.Fatalf("rejectCodexInlineHooksConflict returned error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("rejectCodexInlineHooksConflict returned nil, want error containing %q", test.wantError)
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %q, want substring %q", err.Error(), test.wantError)
			}
		})
	}

	missingResolution := func(destination output.Destination) (string, error) {
		return "", fmt.Errorf("unexpected destination %q", destination)
	}
	if err := ValidateAggregateReadPreconditions(".codex/hooks.json", missingResolution); err == nil {
		t.Fatal("rejectCodexInlineHooksConflict returned nil for resolver failure")
	}
}

func writeTestFile(t *testing.T, root string, relativePath string, content string) {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}

func testDestinationResolver(root string) DestinationResolver {
	return func(destination output.Destination) (string, error) {
		return filepath.Join(root, filepath.FromSlash(string(destination))), nil
	}
}
