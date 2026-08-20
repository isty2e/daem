package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/test/outputtest"
)

func TestRejectCodexInlineHooksConflictIsScopedToCodexHookDestinations(t *testing.T) {
	err := ValidateAggregateReadPreconditions(context.Background(), outputtest.Parse(t, ".claude/settings.json"), func(destination output.Destination) (string, error) {
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
			hookDestination:   outputtest.Parse(t, ".codex/hooks.json"),
			configDestination: ".codex/config.toml",
			configContent:     "[hooks]\n",
			wantError:         `unmanaged Codex inline hooks found in ".codex/config.toml"`,
		},
		{
			name:              "global inline hooks conflict",
			hookDestination:   outputtest.Parse(t, "~/.codex/hooks.json"),
			configDestination: "~/.codex/config.toml",
			configContent:     "hooks = []\n",
			wantError:         `unmanaged Codex inline hooks found in "~/.codex/config.toml"`,
		},
		{
			name:              "unrelated codex config table is allowed",
			hookDestination:   outputtest.Parse(t, ".codex/hooks.json"),
			configDestination: ".codex/config.toml",
			configContent:     "[mcp_servers.context7]\ncommand = \"npx\"\n",
		},
		{
			name:              "missing paired config is allowed",
			hookDestination:   outputtest.Parse(t, ".codex/hooks.json"),
			configDestination: ".codex/config.toml",
		},
		{
			name:              "malformed paired config remains boundary parse error",
			hookDestination:   outputtest.Parse(t, ".codex/hooks.json"),
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

			err := ValidateAggregateReadPreconditions(context.Background(), test.hookDestination, testDestinationResolver(caseDir))
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
	if err := ValidateAggregateReadPreconditions(context.Background(), outputtest.Parse(t, ".codex/hooks.json"), missingResolution); err == nil {
		t.Fatal("rejectCodexInlineHooksConflict returned nil for resolver failure")
	}
}

func TestRejectCodexInlineHooksConflictBoundsTOMLBeforeDecode(t *testing.T) {
	root := t.TempDir()
	destination := outputtest.Parse(t, ".codex/hooks.json")
	configPath := filepath.Join(root, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	deep := "root = " + strings.Repeat("{ k = ", tomlstrict.MaximumDepth) +
		"1" + strings.Repeat(" }", tomlstrict.MaximumDepth) + "\n"
	if err := os.WriteFile(configPath, []byte(deep), 0o600); err != nil {
		t.Fatal(err)
	}

	err := ValidateAggregateReadPreconditions(context.Background(), destination, testDestinationResolver(root))
	if !errors.Is(err, tomlstrict.ErrMaximumDepthExceeded) {
		t.Fatalf("ValidateAggregateReadPreconditions(deep TOML) = %v, want depth rejection", err)
	}

	file, err := os.OpenFile(configPath, os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((4 << 20) + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	err = ValidateAggregateReadPreconditions(context.Background(), destination, testDestinationResolver(root))
	if !errors.Is(err, filesnapshot.ErrLimitExceeded) {
		t.Fatalf("ValidateAggregateReadPreconditions(oversized TOML) = %v, want byte-limit rejection", err)
	}
}

func TestRejectCodexInlineHooksConflictPreservesReferentAndCancellationSemantics(t *testing.T) {
	t.Run("stable final symlink", func(t *testing.T) {
		root := t.TempDir()
		targetPath := filepath.Join(root, "shared-config.toml")
		if err := os.WriteFile(targetPath, []byte("model = \"gpt-5-codex\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(root, ".codex", "config.toml")
		if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(targetPath, configPath); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := ValidateAggregateReadPreconditions(
			context.Background(),
			outputtest.Parse(t, ".codex/hooks.json"),
			testDestinationResolver(root),
		); err != nil {
			t.Fatalf("ValidateAggregateReadPreconditions(symlink) = %v, want nil", err)
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := ValidateAggregateReadPreconditions(
			ctx,
			outputtest.Parse(t, ".codex/hooks.json"),
			func(destination output.Destination) (string, error) {
				t.Fatalf("resolver called after cancellation for %q", destination)
				return "", nil
			},
		)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("ValidateAggregateReadPreconditions(canceled) = %v, want context.Canceled", err)
		}
	})
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
		return filepath.Join(root, filepath.FromSlash(destination.String())), nil
	}
}
