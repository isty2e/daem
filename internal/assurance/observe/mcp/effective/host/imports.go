package host

import (
	"fmt"
	"os"
	"path/filepath"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
)

func observeImport(
	kind importKind,
	id string,
	precedence mcpeffective.RelativePrecedence,
	homeDir string,
	workDir string,
	serverName string,
	sourceKind mcpeffective.SourceKind,
) (mcpeffective.SourceObservation, bool) {
	if kind == importOpenCode {
		return observeOpenCodeImport(id, precedence, homeDir, workDir, serverName, sourceKind)
	}
	for _, path := range importCandidates(kind, homeDir, workDir) {
		content, exists, readErr := readStableRegularFile(path)
		if readErr != nil {
			return mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: id, Path: path, Kind: sourceKind, Precedence: precedence,
				Shared: true, State: mcpeffective.SourceOpaque,
				DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
				Detail:                readErr.Error(),
			}), true
		}
		if !exists {
			continue
		}
		var (
			names map[string]struct{}
			err   error
		)
		if kind == importCodex {
			names, err = decodeCodexImportServerNames(content, filepath.Ext(path) == ".toml")
		} else {
			names, err = decodeJSONImportServerNames(content, kind)
		}
		if err != nil {
			return mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: id, Path: path, Kind: sourceKind, Precedence: precedence,
				Shared: true, State: mcpeffective.SourceOpaque,
				DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
				Detail:                err.Error(),
			}), true
		}
		_, defines := names[serverName]
		return mustSourceObservation(mcpeffective.SourceObservationInput{
			ID: id, Path: path, Kind: sourceKind, Precedence: precedence,
			Shared: true, State: mcpeffective.SourceExact, DefinesSelectedName: defines,
			DefinitionEquivalence: definitionEquivalenceForImportedName(defines),
		}), true
	}
	return mcpeffective.SourceObservation{}, false
}

func observeOpenCodeImport(
	id string,
	precedence mcpeffective.RelativePrecedence,
	homeDir string,
	workDir string,
	serverName string,
	sourceKind mcpeffective.SourceKind,
) (mcpeffective.SourceObservation, bool) {
	merged := make(map[string]map[string]any)
	var highestPath string
	candidates, candidateErr := openCodeImportCandidates(homeDir, workDir)
	if candidateErr != nil {
		return mustSourceObservation(mcpeffective.SourceObservationInput{
			ID: id, Path: filepath.Join(workDir, "opencode.json"),
			Kind: sourceKind, Precedence: precedence, Shared: true,
			State:                 mcpeffective.SourceOpaque,
			DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
			Detail:                candidateErr.Error(),
		}), true
	}
	for _, path := range candidates {
		content, exists, readErr := readStableRegularFile(path)
		if readErr != nil {
			return mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: id, Path: path, Kind: sourceKind, Precedence: precedence,
				Shared: true, State: mcpeffective.SourceOpaque,
				DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
				Detail:                readErr.Error(),
			}), true
		}
		if !exists {
			continue
		}
		entries, decodeErr := decodeOpenCodeConfig(content)
		if decodeErr != nil {
			return mustSourceObservation(mcpeffective.SourceObservationInput{
				ID: id, Path: path, Kind: sourceKind, Precedence: precedence,
				Shared: true, State: mcpeffective.SourceOpaque,
				DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
				Detail:                decodeErr.Error(),
			}), true
		}
		mergeOpenCodeEntries(merged, entries)
		highestPath = path
	}
	if highestPath == "" {
		return mcpeffective.SourceObservation{}, false
	}
	_, defines := openCodeServerNames(merged)[serverName]
	return mustSourceObservation(mcpeffective.SourceObservationInput{
		ID: id, Path: highestPath, Kind: sourceKind, Precedence: precedence,
		Shared: true, State: mcpeffective.SourceExact, DefinesSelectedName: defines,
		DefinitionEquivalence: definitionEquivalenceForImportedName(defines),
	}), true
}

func definitionEquivalenceForImportedName(
	defines bool,
) mcpeffective.DefinitionEquivalence {
	if defines {
		return mcpeffective.DefinitionEquivalenceUnknown
	}
	return mcpeffective.DefinitionEquivalenceNotApplicable
}

func importCandidates(kind importKind, homeDir string, workDir string) []string {
	switch kind {
	case importCursor:
		return []string{filepath.Join(homeDir, ".cursor", "mcp.json")}
	case importClaudeCode:
		return []string{
			filepath.Join(homeDir, ".claude", "mcp.json"),
			filepath.Join(homeDir, ".claude.json"),
			filepath.Join(homeDir, ".claude", "claude_desktop_config.json"),
		}
	case importClaudeDesktop:
		return []string{
			filepath.Join(
				homeDir,
				"Library",
				"Application Support",
				"Claude",
				"claude_desktop_config.json",
			),
		}
	case importCodex:
		return []string{
			filepath.Join(homeDir, ".codex", "config.toml"),
			filepath.Join(homeDir, ".codex", "config.json"),
		}
	case importWindsurf:
		return []string{filepath.Join(homeDir, ".windsurf", "mcp.json")}
	case importVSCode:
		return []string{filepath.Join(workDir, ".vscode", "mcp.json")}
	default:
		return nil
	}
}

func openCodeImportCandidates(homeDir string, workDir string) ([]string, error) {
	projectPath, err := nearestOpenCodeProjectConfig(workDir)
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(homeDir, ".config", "opencode", "opencode.json"),
		projectPath,
	}, nil
}

func nearestOpenCodeProjectConfig(workDir string) (string, error) {
	gitRoot := ""
	for current := workDir; ; current = filepath.Dir(current) {
		exists, err := pathExists(filepath.Join(current, ".git"))
		if err != nil {
			return "", fmt.Errorf("inspect OpenCode project boundary: %w", err)
		}
		if exists {
			gitRoot = current
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	if gitRoot == "" {
		return filepath.Join(workDir, "opencode.json"), nil
	}
	for current := workDir; ; current = filepath.Dir(current) {
		path := filepath.Join(current, "opencode.json")
		exists, err := pathExists(path)
		if err != nil {
			return "", fmt.Errorf("inspect OpenCode project config: %w", err)
		}
		if exists || current == gitRoot {
			return path, nil
		}
	}
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
