package codexplugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

type rawPluginContributionManifest struct {
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Skills     json.RawMessage `json:"skills"`
	MCPServers json.RawMessage `json:"mcpServers"`
	Apps       json.RawMessage `json:"apps"`
	Hooks      json.RawMessage `json:"hooks"`
}

func sourceContributionsFromManifest(
	pluginRoot string,
	artifactIdentity string,
	defaultKey string,
	manifest rawPluginContributionManifest,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	contributions := []observecontribution.SourceContribution{}
	if len(manifest.Skills) > 0 {
		skills, reason := skillContributions(pluginRoot, manifest.Skills)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, skills...)
	}
	if len(manifest.MCPServers) > 0 {
		servers, reason := mcpServerContributions(pluginRoot, manifest.MCPServers)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, servers...)
	}
	if len(manifest.Apps) > 0 {
		apps, reason := appContributions(pluginRoot, defaultContributionKey(manifest.Name, defaultKey), manifest.Apps)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, apps...)
	}
	if len(manifest.Hooks) > 0 {
		hooks, reason := hookContributions(pluginRoot, artifactIdentity, manifest.Hooks)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, hooks...)
	}
	return contributions, observecontribution.SourceContributionReasonNone
}

func skillContributions(pluginRoot string, raw json.RawMessage) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	paths, ok := decodePathList(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	contributions := []observecontribution.SourceContribution{}
	for _, path := range paths {
		resolved, marker, reason := resolveManifestPath(pluginRoot, path)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		skills, reason := skillContributionsFromPath(pluginRoot, resolved, marker)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, skills...)
	}
	return contributions, observecontribution.SourceContributionReasonNone
}

func skillContributionsFromPath(
	pluginRoot string,
	resolved string,
	marker string,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	if pathHasSymlinkComponent(pluginRoot, resolved) {
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	if !info.IsDir() {
		if filepath.Base(resolved) != "SKILL.md" {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape
		}
		key := filepath.Base(filepath.Dir(resolved))
		contribution, reason := sourceContribution(observecontribution.SourceContributionSkill, key, marker)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone
	}

	if isRegularBoundedFile(pluginRoot, filepath.Join(resolved, "SKILL.md")) {
		key := filepath.Base(resolved)
		contribution, reason := sourceContribution(
			observecontribution.SourceContributionSkill,
			key,
			filepath.ToSlash(filepath.Join(marker, "SKILL.md")),
		)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone
	}

	entries, err := os.ReadDir(resolved)
	if err != nil {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable
	}
	contributions := []observecontribution.SourceContribution{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(resolved, entry.Name(), "SKILL.md")
		if !isRegularBoundedFile(pluginRoot, skillPath) {
			continue
		}
		contribution, reason := sourceContribution(
			observecontribution.SourceContributionSkill,
			entry.Name(),
			filepath.ToSlash(filepath.Join(marker, entry.Name(), "SKILL.md")),
		)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, contribution)
	}
	return contributions, observecontribution.SourceContributionReasonNone
}

func mcpServerContributions(pluginRoot string, raw json.RawMessage) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	if keys, ok := decodeObjectKeys(raw); ok {
		return contributionKeys(observecontribution.SourceContributionMCPServer, keys, "mcpServers")
	}

	path, ok := decodeString(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	resolved, marker, reason := resolveManifestPath(pluginRoot, path)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason
	}
	content, err := readBoundedFile(pluginRoot, resolved)
	if err != nil {
		if errors.Is(err, errPathBlocked) {
			return nil, observecontribution.SourceContributionReasonArtifactPathBlocked
		}
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, observecontribution.SourceContributionReasonArtifactMalformed
	}
	rawServers, ok := parsed["mcpServers"]
	if !ok {
		rawServers = content
	}
	keys, ok := decodeObjectKeys(rawServers)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	return contributionKeys(observecontribution.SourceContributionMCPServer, keys, marker)
}

func appContributions(pluginRoot string, key string, raw json.RawMessage) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	path, ok := decodeString(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	resolved, marker, reason := resolveManifestPath(pluginRoot, path)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason
	}
	if reason := regularBoundedFileReason(pluginRoot, resolved); reason != observecontribution.SourceContributionReasonNone {
		return nil, reason
	}
	contribution, reason := sourceContribution(observecontribution.SourceContributionApp, key, marker)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason
	}
	return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone
}

func hookContributions(pluginRoot string, artifactIdentity string, raw json.RawMessage) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	if _, ok := decodeObjectKeys(raw); ok {
		contribution, reason := sourceContribution(observecontribution.SourceContributionHook, "inline", artifactIdentity)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone
	}
	paths, ok := decodePathList(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	contributions := make([]observecontribution.SourceContribution, 0, len(paths))
	for _, path := range paths {
		resolved, marker, reason := resolveManifestPath(pluginRoot, path)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		if reason := regularBoundedFileReason(pluginRoot, resolved); reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		key := filepath.Base(marker)
		contribution, reason := sourceContribution(observecontribution.SourceContributionHook, key, marker)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, contribution)
	}
	return contributions, observecontribution.SourceContributionReasonNone
}

func decodeString(raw json.RawMessage) (string, bool) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	if strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func decodePathList(raw json.RawMessage) ([]string, bool) {
	if value, ok := decodeString(raw); ok {
		return []string{value}, true
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return nil, false
		}
		result = append(result, value)
	}
	return result, true
}

func decodeObjectKeys(raw json.RawMessage) ([]string, bool) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		key = strings.TrimSpace(key)
		if !observecontribution.ValidSourceToken(key) {
			return nil, false
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, true
}

func contributionKeys(
	kind observecontribution.SourceContributionKind,
	keys []string,
	marker string,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	contributions := make([]observecontribution.SourceContribution, 0, len(keys))
	for _, key := range keys {
		contribution, reason := sourceContribution(kind, key, marker)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason
		}
		contributions = append(contributions, contribution)
	}
	return contributions, observecontribution.SourceContributionReasonNone
}

func defaultContributionKey(manifestName string, fallback string) string {
	manifestName = strings.TrimSpace(manifestName)
	if observecontribution.ValidSourceToken(manifestName) {
		return manifestName
	}
	return fallback
}

func sourceContribution(
	kind observecontribution.SourceContributionKind,
	key string,
	marker string,
) (observecontribution.SourceContribution, observecontribution.SourceContributionReason) {
	contribution, err := observecontribution.NewSourceContribution(observecontribution.SourceContributionSpec{
		Kind:         kind,
		Key:          key,
		SourceMarker: marker,
	})
	if err != nil {
		return observecontribution.SourceContribution{}, observecontribution.SourceContributionReasonUnsupportedShape
	}
	return contribution, observecontribution.SourceContributionReasonNone
}

func sortSourceContributions(contributions []observecontribution.SourceContribution) {
	sort.Slice(contributions, func(left int, right int) bool {
		if contributions[left].Kind() != contributions[right].Kind() {
			return contributions[left].Kind() < contributions[right].Kind()
		}
		if contributions[left].Key() != contributions[right].Key() {
			return contributions[left].Key() < contributions[right].Key()
		}
		return contributions[left].SourceMarker() < contributions[right].SourceMarker()
	})
}
