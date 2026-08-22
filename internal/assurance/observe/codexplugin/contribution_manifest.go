package codexplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
)

var (
	errJSONShape     = errors.New("unsupported JSON shape")
	errJSONDepth     = errors.New("JSON nesting exceeds observation depth")
	errJSONDuplicate = errors.New("duplicate JSON object key")
	errJSONBudget    = errors.New("JSON structure exceeds observation budget")
)

const maximumObservationJSONDepth = 128

type rawPluginContributionManifest struct {
	Name       string          `json:"name"`
	Version    string          `json:"version"`
	Skills     json.RawMessage `json:"skills"`
	MCPServers json.RawMessage `json:"mcpServers"`
	Apps       json.RawMessage `json:"apps"`
	Hooks      json.RawMessage `json:"hooks"`
}

func decodePluginContributionManifest(
	content []byte,
	budget *observationBudget,
) (rawPluginContributionManifest, observecontribution.SourceContributionReason) {
	decoder := observationJSONDecoder(content)
	if err := consumeJSONDelim(decoder, '{'); err != nil {
		return rawPluginContributionManifest{}, observecontribution.SourceContributionReasonArtifactMalformed
	}

	manifest := rawPluginContributionManifest{}
	seen := make(map[string]struct{})
	for decoder.More() {
		key, err := nextUniqueJSONObjectKey(decoder, seen, budget)
		if err != nil {
			return rawPluginContributionManifest{}, jsonSkipReason(err)
		}
		switch key {
		case "name":
			err = decoder.Decode(&manifest.Name)
		case "version":
			err = decoder.Decode(&manifest.Version)
		case "skills":
			err = decoder.Decode(&manifest.Skills)
		case "mcpServers":
			err = decoder.Decode(&manifest.MCPServers)
		case "apps":
			err = decoder.Decode(&manifest.Apps)
		case "hooks":
			err = decoder.Decode(&manifest.Hooks)
		default:
			err = skipJSONValue(decoder, 1, budget)
		}
		if err != nil {
			return rawPluginContributionManifest{}, jsonSkipReason(err)
		}
	}
	if err := consumeJSONDelim(decoder, '}'); err != nil || !jsonEOF(decoder) {
		return rawPluginContributionManifest{}, observecontribution.SourceContributionReasonArtifactMalformed
	}
	return manifest, observecontribution.SourceContributionReasonNone
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
	paths, reason := decodePathList(raw, plugin.budget)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
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
			true,
		)
	case childDirectory:
	default:
		return nil, observecontribution.SourceContributionReasonUnsupportedShape, nil
	}

	skillPath := filepath.ToSlash(filepath.Join(relative, "SKILL.md"))
	_, exists, reason, err := plugin.snapshot(ctx, skillPath)
	if observationCanceled(err) {
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	if err != nil {
		reason, err = classifyDirectoryError(err)
		if observationCanceled(err) {
			return nil, observecontribution.SourceContributionReasonNone, err
		}
	}
	switch {
	case reason == observecontribution.SourceContributionReasonNone && exists:
		return emitContribution(
			plugin.budget,
			observecontribution.SourceContributionSkill,
			filepath.Base(relative),
			skillPath,
			true,
		)
	case failClosedSkillInspection(reason):
		return nil, reason, err
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
		if err != nil || failClosedSkillInspection(reason) {
			return nil, reason, err
		}
		if kind == childSymlink || skipNestedSkillReason(reason) || kind != childDirectory {
			continue
		}
		childSkill := filepath.ToSlash(filepath.Join(relative, name, "SKILL.md"))
		_, exists, reason, err := plugin.snapshot(ctx, childSkill)
		if observationCanceled(err) {
			return nil, observecontribution.SourceContributionReasonNone, err
		}
		if err != nil {
			reason, err = classifyDirectoryError(err)
			if observationCanceled(err) {
				return nil, observecontribution.SourceContributionReasonNone, err
			}
		}
		if failClosedSkillInspection(reason) {
			return nil, reason, err
		}
		if skipNestedSkillReason(reason) || !exists {
			continue
		}
		emitted, reason, err := emitContribution(
			plugin.budget,
			observecontribution.SourceContributionSkill,
			name,
			childSkill,
			true,
		)
		if err != nil || reason != observecontribution.SourceContributionReasonNone {
			return nil, reason, err
		}
		contributions = append(contributions, emitted...)
	}
	return contributions, observecontribution.SourceContributionReasonNone, nil
}

func skipNestedSkillReason(reason observecontribution.SourceContributionReason) bool {
	return reason == observecontribution.SourceContributionReasonArtifactPathBlocked ||
		reason == observecontribution.SourceContributionReasonUnsupportedShape
}

func failClosedSkillInspection(reason observecontribution.SourceContributionReason) bool {
	return reason != observecontribution.SourceContributionReasonNone && !skipNestedSkillReason(reason)
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
	keys, reason := decodeObjectKeys(raw, plugin.budget)
	if reason == observecontribution.SourceContributionReasonNone {
		return contributionKeys(plugin.budget, observecontribution.SourceContributionMCPServer, keys, "mcpServers")
	}
	if reason != observecontribution.SourceContributionReasonUnsupportedShape {
		return nil, reason, nil
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
	keys, reason = decodeReferencedMCPServerKeys(content, plugin.budget)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
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
	return emitContribution(plugin.budget, observecontribution.SourceContributionApp, key, relative, true)
}

func hookContributions(
	ctx context.Context,
	plugin *pluginObservation,
	artifactIdentity string,
	raw json.RawMessage,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	inline, reason := decodeInlineHookObject(raw, plugin.budget)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
	}
	if inline {
		return emitContribution(plugin.budget, observecontribution.SourceContributionHook, "inline", artifactIdentity, true)
	}
	paths, reason := decodePathList(raw, plugin.budget)
	if reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, nil
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
			true,
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

func decodePathList(
	raw json.RawMessage,
	budget *observationBudget,
) ([]string, observecontribution.SourceContributionReason) {
	decoder := observationJSONDecoder(raw)
	token, err := decoder.Token()
	if err != nil {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	if value, ok := token.(string); ok {
		if strings.TrimSpace(value) == "" || !jsonEOF(decoder) {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape
		}
		if budget.consumeNames([]string{value}) {
			return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded
		}
		return []string{value}, observecontribution.SourceContributionReasonNone
	}
	if token != json.Delim('[') {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	paths := []string{}
	for decoder.More() {
		item, err := decoder.Token()
		if err != nil {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape
		}
		value, ok := item.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape
		}
		if budget.consumeNames([]string{value}) {
			return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded
		}
		paths = append(paths, value)
	}
	if err := consumeJSONDelim(decoder, ']'); err != nil || !jsonEOF(decoder) {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	return paths, observecontribution.SourceContributionReasonNone
}

func decodeObjectKeys(
	raw json.RawMessage,
	budget *observationBudget,
) ([]string, observecontribution.SourceContributionReason) {
	decoder := observationJSONDecoder(raw)
	keys, err := decodeJSONObjectKeys(decoder, budget)
	if err != nil {
		return nil, jsonSkipReason(err)
	}
	if !jsonEOF(decoder) {
		return nil, observecontribution.SourceContributionReasonUnsupportedShape
	}
	return admitContributionKeys(keys, budget)
}

func decodeJSONObjectKeys(
	decoder *json.Decoder,
	budget *observationBudget,
) ([]string, error) {
	if err := consumeJSONDelim(decoder, '{'); err != nil {
		return nil, err
	}
	keys := []string{}
	seen := make(map[string]struct{})
	for decoder.More() {
		key, err := nextUniqueJSONObjectKey(decoder, seen, budget)
		if err != nil {
			return nil, err
		}
		if err := skipJSONValue(decoder, 1, budget); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := consumeJSONDelim(decoder, '}'); err != nil {
		return nil, err
	}
	return keys, nil
}

func admitContributionKeys(
	keys []string,
	budget *observationBudget,
) ([]string, observecontribution.SourceContributionReason) {
	for index, key := range keys {
		key = strings.TrimSpace(key)
		if !observecontribution.ValidSourceToken(key) {
			return nil, observecontribution.SourceContributionReasonUnsupportedShape
		}
		if budget.consumeNames([]string{key}) {
			return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded
		}
		keys[index] = key
	}
	sort.Strings(keys)
	return keys, observecontribution.SourceContributionReasonNone
}

func decodeReferencedMCPServerKeys(
	content []byte,
	budget *observationBudget,
) ([]string, observecontribution.SourceContributionReason) {
	decoder := observationJSONDecoder(content)
	if err := consumeJSONDelim(decoder, '{'); err != nil {
		return nil, observecontribution.SourceContributionReasonArtifactMalformed
	}
	directKeys := []string{}
	var wrappedKeys []string
	foundWrapper := false
	seen := make(map[string]struct{})
	for decoder.More() {
		key, err := nextUniqueJSONObjectKey(decoder, seen, budget)
		if err != nil {
			return nil, jsonSkipReason(err)
		}
		if key == "mcpServers" {
			wrappedKeys, err = decodeJSONObjectKeys(decoder, budget)
			if err != nil {
				return nil, jsonSkipReason(err)
			}
			foundWrapper = true
			continue
		}
		if err := skipJSONValue(decoder, 1, budget); err != nil {
			return nil, jsonSkipReason(err)
		}
		if !foundWrapper {
			directKeys = append(directKeys, key)
		}
	}
	if err := consumeJSONDelim(decoder, '}'); err != nil || !jsonEOF(decoder) {
		return nil, observecontribution.SourceContributionReasonArtifactMalformed
	}
	if foundWrapper {
		return admitContributionKeys(wrappedKeys, budget)
	}
	return admitContributionKeys(directKeys, budget)
}

func decodeInlineHookObject(
	raw json.RawMessage,
	budget *observationBudget,
) (bool, observecontribution.SourceContributionReason) {
	decoder := observationJSONDecoder(raw)
	opening, err := decoder.Token()
	if err != nil {
		return false, observecontribution.SourceContributionReasonArtifactMalformed
	}
	if opening != json.Delim('{') {
		return false, observecontribution.SourceContributionReasonNone
	}
	seen := make(map[string]struct{})
	for decoder.More() {
		key, err := nextUniqueJSONObjectKey(decoder, seen, budget)
		if err != nil {
			return true, jsonSkipReason(err)
		}
		if !observecontribution.ValidSourceToken(strings.TrimSpace(key)) {
			return true, observecontribution.SourceContributionReasonUnsupportedShape
		}
		if err := skipJSONValue(decoder, 1, budget); err != nil {
			return true, jsonSkipReason(err)
		}
	}
	if err := consumeJSONDelim(decoder, '}'); err != nil || !jsonEOF(decoder) {
		return true, observecontribution.SourceContributionReasonArtifactMalformed
	}
	return true, observecontribution.SourceContributionReasonNone
}

func observationJSONDecoder(data []byte) *json.Decoder {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder
}

func nextUniqueJSONObjectKey(
	decoder *json.Decoder,
	seen map[string]struct{},
	budget *observationBudget,
) (string, error) {
	keyToken, err := decoder.Token()
	if err != nil {
		return "", err
	}
	key, ok := keyToken.(string)
	if !ok {
		return "", errJSONShape
	}
	if budget.consumeJSONObjectKey(key) {
		return "", errJSONBudget
	}
	if _, duplicate := seen[key]; duplicate {
		return "", errJSONDuplicate
	}
	seen[key] = struct{}{}
	return key, nil
}

func skipJSONValue(
	decoder *json.Decoder,
	depth int,
	budget *observationBudget,
) error {
	if depth > maximumObservationJSONDepth {
		return errJSONDepth
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			if _, err := nextUniqueJSONObjectKey(decoder, seen, budget); err != nil {
				return err
			}
			if err := skipJSONValue(decoder, depth+1, budget); err != nil {
				return err
			}
		}
		return consumeJSONDelim(decoder, '}')
	case '[':
		for decoder.More() {
			if err := skipJSONValue(decoder, depth+1, budget); err != nil {
				return err
			}
		}
		return consumeJSONDelim(decoder, ']')
	default:
		return errJSONShape
	}
}

func consumeJSONDelim(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != want {
		return errJSONShape
	}
	return nil
}

func jsonEOF(decoder *json.Decoder) bool {
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func jsonSkipReason(err error) observecontribution.SourceContributionReason {
	if errors.Is(err, errJSONBudget) {
		return observecontribution.SourceContributionReasonArtifactBudgetExceeded
	}
	if errors.Is(err, errJSONDepth) || errors.Is(err, errJSONShape) {
		return observecontribution.SourceContributionReasonUnsupportedShape
	}
	return observecontribution.SourceContributionReasonArtifactMalformed
}

func contributionKeys(
	budget *observationBudget,
	kind observecontribution.SourceContributionKind,
	keys []string,
	marker string,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	contributions := make([]observecontribution.SourceContribution, 0, len(keys))
	for _, key := range keys {
		emitted, reason, err := emitContribution(budget, kind, key, marker, false)
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
	chargeKey bool,
) ([]observecontribution.SourceContribution, observecontribution.SourceContributionReason, error) {
	if chargeKey && budget.consumeNames([]string{key}) {
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
