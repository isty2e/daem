// Package pihostpath resolves Pi's host-selected physical output roots.
package pihostpath

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
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
	var home string
	if root == "" || root == "~" || strings.HasPrefix(root, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(root, `~\`) {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve Pi config home: %w", err)
		}
	}
	workDir := input.WorkDir
	if workDir == "" && root != "" && root != "~" && !strings.HasPrefix(root, "~/") && !(runtime.GOOS == "windows" && strings.HasPrefix(root, `~\`)) && !strings.HasPrefix(root, "file://") && !filepath.IsAbs(root) {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve Pi config working directory: %w", err)
		}
	}
	return nativehostpath.PiAgentDirectory(root, workDir, home)
}

// DestinationOverride maps Pi's logical agent root and legacy global MCP
// destination to the current host-selected physical root.
func DestinationOverride(workDir string) hostpath.DestinationOverrideResolver {
	return func(destination output.Destination) (string, bool, error) {
		if destination.RootRole() == output.RootPiAgent {
			root, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
			if err != nil {
				return "", true, err
			}
			return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), true, nil
		}
		if destination == globalMCPLogicalDestination {
			root, err := ResolveAgentRoot(AgentRootInput{WorkDir: workDir})
			if err != nil {
				return "", true, err
			}
			return filepath.Join(root, "mcp.json"), true, nil
		}
		return "", false, nil
	}
}

func mustDestination(value string) output.Destination {
	destination, err := output.Parse(value)
	if err != nil {
		panic(err)
	}
	return destination
}
