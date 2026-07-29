package authoring

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestMCPServerAddWarningEdgeHuntRoundOne(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		args        []string
		wantWarning string
	}{
		{
			name:        "npx package flag floating",
			command:     "npx",
			args:        []string{"--package", "@scope/server", "server-bin"},
			wantWarning: `floating delegated npm package "@scope/server"`,
		},
		{
			name:    "npx package flag pinned",
			command: "npx",
			args:    []string{"--package", "@scope/server@1.2.3", "server-bin"},
		},
		{
			name:        "npx latest selector is floating",
			command:     "npx",
			args:        []string{"-y", "@scope/server@latest"},
			wantWarning: `floating delegated npm package "@scope/server"`,
		},
		{
			name:    "plain command does not parse package-looking args",
			command: "node",
			args:    []string{"server@latest"},
		},
		{
			name:    "invalid package-backed command does not fabricate warning",
			command: "npx",
			args:    []string{"--yes"},
		},
		{
			name:    "pinned scoped package with separator passthrough",
			command: "npx",
			args:    []string{"--yes", "--", "@scope/server@3.0.0"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMCPAddWarnings(t, test.command, test.args, test.wantWarning)
		})
	}
}

func TestMCPServerAddWarningEdgeHuntRoundTwo(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		args        []string
		wantWarning string
	}{
		{
			name:        "uvx unpinned package floats",
			command:     "uvx",
			args:        []string{"mcp-server"},
			wantWarning: `floating delegated python package "mcp-server"`,
		},
		{
			name:    "uvx exact package is pinned",
			command: "uvx",
			args:    []string{"mcp-server==0.4.0"},
		},
		{
			name:        "docker latest tag floats",
			command:     "docker",
			args:        []string{"run", "ghcr.io/acme/mcp:latest"},
			wantWarning: `floating delegated container package "ghcr.io/acme/mcp"`,
		},
		{
			name:    "docker digest is pinned",
			command: "docker",
			args:    []string{"run", "ghcr.io/acme/mcp@sha256:abcdef"},
		},
		{
			name:    "docker image option value skipped before image",
			command: "docker",
			args:    []string{"run", "--name", "context7", "ghcr.io/acme/mcp@sha256:abcdef"},
		},
		{
			name:        "docker image without tag floats",
			command:     "docker",
			args:        []string{"run", "--rm", "ghcr.io/acme/mcp"},
			wantWarning: `floating delegated container package "ghcr.io/acme/mcp"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMCPAddWarnings(t, test.command, test.args, test.wantWarning)
		})
	}
}

func TestMCPServerAddWarningEdgeHuntRoundThree(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		args        []string
		wantWarning string
	}{
		{
			name:        "env references do not leak into warning",
			command:     "npx",
			args:        []string{"@scope/server"},
			wantWarning: `floating delegated npm package "@scope/server"`,
		},
		{
			name:        "warning derived from normalized default targets",
			command:     "npx",
			args:        []string{"@scope/default-target-server"},
			wantWarning: `floating delegated npm package "@scope/default-target-server"`,
		},
		{
			name:    "pinned package with pre-release selector is pinned",
			command: "npx",
			args:    []string{"@scope/server@1.2.3-beta.1"},
		},
		{
			name:    "plain uvx-looking arg under plain runner stays plain",
			command: "python",
			args:    []string{"mcp-server==0.4.0"},
		},
		{
			name:        "docker long option with equals does not hide floating image",
			command:     "docker",
			args:        []string{"run", "--platform=linux/arm64", "ghcr.io/acme/mcp"},
			wantWarning: `floating delegated container package "ghcr.io/acme/mcp"`,
		},
		{
			name:    "docker short option value skipped before pinned image",
			command: "docker",
			args:    []string{"run", "-p", "9000:9000", "ghcr.io/acme/mcp:1.0.0"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertMCPAddWarnings(t, test.command, test.args, test.wantWarning)
		})
	}
}

func TestMCPServerAddWarningUsesActualCanonicalPlacement(t *testing.T) {
	for _, placement := range aggregate.ImplementedMCPPlacements() {
		t.Run(string(placement.Target())+"/"+string(placement.Scope()), func(t *testing.T) {
			change, err := BuildAddMCPServerChange(ManifestDocument{
				Content: []byte("version = 1\ntargets = [\"" + string(placement.Target()) + "\"]\n"),
			}, AddMCPServerRequest{
				Name:    "context7",
				Command: "npx",
				Args:    []string{"-y", "@scope/server"},
				Targets: []string{string(placement.Target())},
				Scope:   string(placement.Scope()),
			})
			if err != nil {
				t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
			}
			if len(change.Warnings) == 0 || !strings.Contains(change.Warnings[0], `floating delegated npm package "@scope/server"`) {
				t.Fatalf("warnings = %#v, want actual placement floating-package warning", change.Warnings)
			}
		})
	}
}

func assertMCPAddWarnings(t *testing.T, command string, args []string, wantWarning string) {
	t.Helper()

	change, err := BuildAddMCPServerChange(ManifestDocument{
		Content: []byte("version = 1\ntargets = [\"claude-code\"]\n"),
	}, AddMCPServerRequest{
		Name:    "context7",
		Command: command,
		Args:    args,
		Env: []MCPServerEnvAssignment{
			{Name: "SERVER_TOKEN", FromEnv: "HOST_TOKEN"},
		},
	})
	if err != nil {
		t.Fatalf("BuildAddMCPServerChange returned error: %v", err)
	}

	if wantWarning == "" {
		if len(change.Warnings) != 0 {
			t.Fatalf("Warnings = %#v, want none", change.Warnings)
		}
		return
	}
	if len(change.Warnings) != 1 {
		t.Fatalf("Warnings = %#v, want one warning", change.Warnings)
	}
	warning := change.Warnings[0]
	if !strings.Contains(warning, wantWarning) {
		t.Fatalf("warning = %q, want %q", warning, wantWarning)
	}
	if strings.Contains(warning, "SERVER_TOKEN") || strings.Contains(warning, "HOST_TOKEN") {
		t.Fatalf("warning = %q, want no env names or values", warning)
	}
}
