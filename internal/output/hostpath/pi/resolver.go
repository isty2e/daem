// Package pihostpath resolves Pi's host-selected physical output roots.
package pihostpath

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

const AgentRootEnvironmentVariable = "PI_CODING_AGENT_DIR"

var globalMCPLogicalDestination = mustDestination(
	aggregate.PiGlobalMCPConfigPath,
)

// AgentRootInput supplies the operation-selected working directory and an
// optional explicit root used by passive observers and tests.
type AgentRootInput struct {
	ExplicitRoot string
	WorkDir      string
}

// ResolveAgentRoot returns Pi's current physical agent configuration root
// without resolving symlinks. Relative environment values follow Pi's
// process-working-directory semantics.
func ResolveAgentRoot(input AgentRootInput) (string, error) {
	root := input.ExplicitRoot
	if root == "" {
		root = os.Getenv(AgentRootEnvironmentVariable)
	}
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve Pi config home: %w", err)
		}
		root = filepath.Join(home, ".pi", "agent")
	}
	if root == "~" || strings.HasPrefix(root, "~"+string(os.PathSeparator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expand Pi config home: %w", err)
		}
		if root == "~" {
			root = home
		} else {
			root = filepath.Join(home, strings.TrimPrefix(root, "~"+string(os.PathSeparator)))
		}
	}
	if !filepath.IsAbs(root) {
		base := input.WorkDir
		if base == "" {
			var err error
			base, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("resolve Pi config working directory: %w", err)
			}
		}
		if strings.TrimSpace(base) == "" || strings.TrimSpace(base) != base || !filepath.IsAbs(base) {
			return "", fmt.Errorf("Pi config working directory must be a trimmed absolute path")
		}
		root = filepath.Join(base, root)
	}
	if strings.TrimSpace(root) == "" || strings.TrimSpace(root) != root {
		return "", fmt.Errorf("Pi config root must be non-empty and trimmed")
	}
	if strings.ContainsRune(root, '\x00') {
		return "", fmt.Errorf("Pi config root must not contain a NUL byte")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("Pi config root must be absolute")
	}
	return filepath.Clean(root), nil
}

// DestinationOverride maps Pi's logical global MCP destination to the current
// host-selected physical root.
func DestinationOverride(workDir string) hostpath.DestinationOverrideResolver {
	return func(destination output.Destination) (string, bool, error) {
		if destination != globalMCPLogicalDestination {
			return "", false, nil
		}
		root, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
		if err != nil {
			return "", true, err
		}
		return filepath.Join(root, "mcp.json"), true, nil
	}
}

func mustDestination(value string) output.Destination {
	destination, err := output.Parse(value)
	if err != nil {
		panic(err)
	}
	return destination
}
