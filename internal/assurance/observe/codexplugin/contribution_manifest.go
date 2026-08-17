package codexplugin

import (
	"context"
	"encoding/json"
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
	ctx context.Context,
	pluginRoot string,
	artifactIdentity string,
	defaultKey string,
	manifest rawPluginContributionManifest,
	budget *observationBudget,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	contributions := []observecontribution.SourceContribution{}
	if len(manifest.Skills) > 0 {
		skills, reason, err := skillContributions(ctx, pluginRoot, manifest.Skills, budget)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, skills...)
	}
	if len(manifest.MCPServers) > 0 {
		servers, reason, err := mcpServerContributions(ctx, pluginRoot, manifest.MCPServers)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, servers...)
	}
	if len(manifest.Apps) > 0 {
		apps, reason, err := appContributions(ctx, pluginRoot, defaultContributionKey(manifest.Name, defaultKey), manifest.Apps)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, apps...)
	}
	if len(manifest.Hooks) > 0 {
		hooks, reason, err := hookContributions(ctx, pluginRoot, artifactIdentity, manifest.Hooks)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, hooks...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func skillContributions(
	ctx context.Context,
	pluginRoot string,
	raw json.RawMessage,
	budget *observationBudget,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	paths, ok := decodePathList(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	contributions := []observecontribution.SourceContribution{}
	for _, path := range paths {
		resolved, marker, reason := resolveManifestPath(pluginRoot, path)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		skills, reason, err := skillContributionsFromPath(ctx, pluginRoot, resolved, marker, budget)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, skills...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func skillContributionsFromPath(
	ctx context.Context,
	pluginRoot string,
	resolved string,
	marker string,
	budget *observationBudget,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if pathHasSymlinkComponent(pluginRoot, resolved) {
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	if !info.IsDir() {
		if filepath.Base(resolved) != "SKILL.md" {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
		}
		_, exists, reason, err := snapshotContainedFile(ctx, pluginRoot, resolved)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		if !exists {
			return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
		}
		key := filepath.Base(filepath.Dir(resolved))
		contribution, reason := sourceContribution(observecontribution.SourceContributionSkill, key, marker)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone, nil
	}

	_, exists, reason, err := snapshotContainedFile(ctx, pluginRoot, filepath.Join(resolved, "SKILL.md"))
	if err != nil {
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	switch reason {
	case observecontribution.SourceContributionReasonNone:
		if exists {
			key := filepath.Base(resolved)
			contribution, reason := sourceContribution(
				observecontribution.SourceContributionSkill,
				key,
				filepath.ToSlash(filepath.Join(marker, "SKILL.md")),
			)
			if reason != observecontribution.SourceContributionReasonNone {
				return nil, reason, nil
			}
			return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone, nil
		}
	case observecontribution.SourceContributionReasonArtifactUnstable,
		observecontribution.SourceContributionReasonArtifactBudgetExceeded:
		return nil, reason, nil
	}

	names, reason, err := listContainedDirectoryNames(ctx, pluginRoot, resolved, budget)
	if err != nil {
		if directoryMissing(err) {
			return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
		}
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	contributions := []observecontribution.SourceContribution{}
	for _, name := range names {
		child := filepath.Join(resolved, name)
		info, statErr := os.Lstat(child)
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		skillPath := filepath.Join(child, "SKILL.md")
		_, exists, reason, err := snapshotContainedFile(ctx, pluginRoot, skillPath)
		if err != nil {
			return nil, observecontribution.SourceContributionReasonNone, err
		}
		switch reason {
		case observecontribution.SourceContributionReasonArtifactUnstable,
			observecontribution.SourceContributionReasonArtifactBudgetExceeded:
			return nil, reason, nil
		case observecontribution.SourceContributionReasonNone:
			if !exists {
				continue
			}
		default:
			continue
		}
		contribution, reason := sourceContribution(
			observecontribution.SourceContributionSkill,
			name,
			filepath.ToSlash(filepath.Join(marker, name, "SKILL.md")),
		)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		contributions = append(contributions, contribution)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func mcpServerContributions(
	ctx context.Context,
	pluginRoot string,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if keys, ok := decodeObjectKeys(raw); ok {
		contributions, reason := contributionKeys(observecontribution.SourceContributionMCPServer, keys, "mcpServers")
		return contributions, reason, nil
	}

	path, ok := decodeString(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	resolved, marker, reason := resolveManifestPath(pluginRoot, path)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	content, reason, err := requiredContainedFile(ctx, pluginRoot, resolved)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, observecontribution.SourceContributionReasonArtifactMalformed, nil
	}
	rawServers, ok := parsed["mcpServers"]
	if !ok {
		rawServers = content
	}
	keys, ok := decodeObjectKeys(rawServers)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	contributions, reason := contributionKeys(observecontribution.SourceContributionMCPServer, keys, marker)
	return contributions, reason, nil
}

func appContributions(
	ctx context.Context,
	pluginRoot string,
	key string,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	path, ok := decodeString(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	resolved, marker, reason := resolveManifestPath(pluginRoot, path)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	if _, reason, err := requiredContainedFile(ctx, pluginRoot, resolved); err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	contribution, reason := sourceContribution(observecontribution.SourceContributionApp, key, marker)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone, nil
}

func hookContributions(
	ctx context.Context,
	pluginRoot string,
	artifactIdentity string,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if _, ok := decodeObjectKeys(raw); ok {
		contribution, reason := sourceContribution(observecontribution.SourceContributionHook, "inline", artifactIdentity)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone, nil
	}
	paths, ok := decodePathList(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	contributions := make([]observecontribution.SourceContribution, 0, len(paths))
	for _, path := range paths {
		resolved, marker, reason := resolveManifestPath(pluginRoot, path)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		if _, reason, err := requiredContainedFile(ctx, pluginRoot, resolved); err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		key := filepath.Base(marker)
		contribution, reason := sourceContribution(observecontribution.SourceContributionHook, key, marker)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		contributions = append(contributions, contribution)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
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
