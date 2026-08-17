package codexplugin

import (
	"context"
	"encoding/json"
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
	plugin *pluginObservation,
	artifactIdentity string,
	defaultKey string,
	manifest rawPluginContributionManifest,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	contributions := []observecontribution.SourceContribution{}
	if len(manifest.Skills) > 0 {
		skills, reason, err := skillContributions(ctx, plugin, manifest.Skills)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, skills...)
	}
	if len(manifest.MCPServers) > 0 {
		servers, reason, err := mcpServerContributions(ctx, plugin, manifest.MCPServers)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, servers...)
	}
	if len(manifest.Apps) > 0 {
		apps, reason, err := appContributions(ctx, plugin, defaultContributionKey(manifest.Name, defaultKey), manifest.Apps)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, apps...)
	}
	if len(manifest.Hooks) > 0 {
		hooks, reason, err := hookContributions(ctx, plugin, artifactIdentity, manifest.Hooks)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, hooks...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func skillContributions(
	ctx context.Context,
	plugin *pluginObservation,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	paths, ok := decodePathList(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	if plugin.budget.consumeNames(paths) {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	contributions := []observecontribution.SourceContribution{}
	for _, path := range paths {
		relative, reason := resolveManifestPath(path)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		skills, reason, err := skillContributionsFromPath(ctx, plugin, relative)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, skills...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func skillContributionsFromPath(
	ctx context.Context,
	plugin *pluginObservation,
	relative string,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	kind, reason, err := plugin.classify(relative)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	switch kind {
	case childMissing:
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
	case childSymlink:
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	case childFile:
		if filepath.Base(relative) != "SKILL.md" {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
		}
		_, exists, reason, err := plugin.snapshot(ctx, relative)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		if !exists {
			return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
		}
		return emitContribution(
			plugin.budget,
			observecontribution.SourceContributionSkill,
			filepath.Base(filepath.Dir(relative)),
			relative,
		)
	case childDirectory:
	default:
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}

	skillPath := filepath.ToSlash(filepath.Join(relative, "SKILL.md"))
	_, exists, reason, err := plugin.snapshot(ctx, skillPath)
	if err != nil {
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	if exists {
		return emitContribution(
			plugin.budget,
			observecontribution.SourceContributionSkill,
			filepath.Base(relative),
			skillPath,
		)
	}

	childRoot, reason, err := walkRelativeDirectory(plugin, relative)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	defer childRoot.close()
	names, reason, err := childRoot.listNames(ctx)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	contributions := []observecontribution.SourceContribution{}
	for _, name := range names {
		kind, reason, err := childRoot.classify(name)
		if err != nil || (reason != observecontribution.SourceContributionReasonNone &&
			reason != observecontribution.SourceContributionReasonArtifactPathBlocked) {
			return nil, reason, err
		}
		if kind == childSymlink || reason == observecontribution.SourceContributionReasonArtifactPathBlocked {
			continue
		}
		if kind != childDirectory {
			continue
		}
		childSkill := filepath.ToSlash(filepath.Join(relative, name, "SKILL.md"))
		_, exists, reason, err := plugin.snapshot(ctx, childSkill)
		if err != nil {
			return nil, observecontribution.SourceContributionReasonNone, err
		}
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		if !exists {
			continue
		}
		emitted, reason, err := emitContribution(
			plugin.budget,
			observecontribution.SourceContributionSkill,
			name,
			childSkill,
		)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, emitted...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func walkRelativeDirectory(
	plugin *pluginObservation,
	relative string,
) (*pluginObservation, observecontribution.SourceContributionReason, error) {
	current := plugin
	for _, part := range strings.Split(relative, "/") {
		child, reason, err := current.openChildDirectory(part)
		if current != plugin {
			current.close()
		}
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		current = child
	}
	return current, observecontribution.SourceContributionReasonNone, nil
}

func mcpServerContributions(
	ctx context.Context,
	plugin *pluginObservation,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if keys, ok := decodeObjectKeys(raw); ok {
		if plugin.budget.consumeNames(keys) {
			return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
		}
		return contributionKeys(plugin.budget, observecontribution.SourceContributionMCPServer, keys, "mcpServers")
	}

	path, ok := decodeString(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	if plugin.budget.consumeNames([]string{path}) {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	relative, reason := resolveManifestPath(path)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	content, reason, err := plugin.requiredFile(ctx, relative)
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
	if plugin.budget.consumeNames(keys) {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	return contributionKeys(plugin.budget, observecontribution.SourceContributionMCPServer, keys, relative)
}

func appContributions(
	ctx context.Context,
	plugin *pluginObservation,
	key string,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	path, ok := decodeString(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	if plugin.budget.consumeNames([]string{path}) {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	relative, reason := resolveManifestPath(path)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	if _, reason, err := plugin.requiredFile(ctx, relative); err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	return emitContribution(plugin.budget, observecontribution.SourceContributionApp, key, relative)
}

func hookContributions(
	ctx context.Context,
	plugin *pluginObservation,
	artifactIdentity string,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if _, ok := decodeObjectKeys(raw); ok {
		return emitContribution(plugin.budget, observecontribution.SourceContributionHook, "inline", artifactIdentity)
	}
	paths, ok := decodePathList(raw)
	if !ok {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}
	if plugin.budget.consumeNames(paths) {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	contributions := make([]observecontribution.SourceContribution, 0, len(paths))
	for _, path := range paths {
		relative, reason := resolveManifestPath(path)
		if reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, nil
		}
		if _, reason, err := plugin.requiredFile(ctx, relative); err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		emitted, reason, err := emitContribution(
			plugin.budget,
			observecontribution.SourceContributionHook,
			filepath.Base(relative),
			relative,
		)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, emitted...)
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
	budget *observationBudget,
	kind observecontribution.SourceContributionKind,
	keys []string,
	marker string,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	contributions := make([]observecontribution.SourceContribution, 0, len(keys))
	for _, key := range keys {
		emitted, reason, err := emitContribution(budget, kind, key, marker)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, emitted...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func emitContribution(
	budget *observationBudget,
	kind observecontribution.SourceContributionKind,
	key string,
	marker string,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if budget.consumeNames([]string{key}) {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	contribution, reason := sourceContribution(kind, key, marker)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	return []observecontribution.SourceContribution{contribution}, observecontribution.SourceContributionReasonNone, nil
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
