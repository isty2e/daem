package hook

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const declarationHookTypeCommand = "command"

type importHookGroup struct {
	Matcher string            `json:"matcher"`
	Hooks   []json.RawMessage `json:"hooks"`
}

type importHookHandler struct {
	Type          string `json:"type"`
	Command       string `json:"command"`
	Condition     string `json:"if"`
	Async         bool   `json:"async"`
	Timeout       int    `json:"timeout"`
	StatusMessage string `json:"statusMessage"`
}

func Candidates(target targetpkg.Target, scope targetpkg.Scope) ([]adopt.Hook, []adopt.Skipped, error) {
	liveDestination, ok := commandhook.Destination(target, scope)
	if !ok {
		return nil, []adopt.Skipped{adopt.UnsupportedSurfaceSkip(target, scope, "hooks")}, nil
	}
	inlineSkipped, err := importCodexInlineHookSkips(target, scope, liveDestination)
	if err != nil {
		return nil, nil, err
	}
	livePath, err := adopt.ResolveDestination(liveDestination)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(livePath)
	if os.IsNotExist(err) {
		skipped := append([]adopt.Skipped{{LivePath: liveDestination, Reason: "missing"}}, inlineSkipped...)
		return nil, skipped, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read live hook path %q: %w", liveDestination, err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		skipped := append([]adopt.Skipped{{LivePath: liveDestination, Reason: "not_regular_file"}}, inlineSkipped...)
		return nil, skipped, nil
	}

	content, err := os.ReadFile(livePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read live hook path %q: %w", liveDestination, err)
	}

	hooks, skipped := parseImportHooks(content, target, scope, liveDestination)
	skipped = append(skipped, inlineSkipped...)
	return hooks, skipped, nil
}

func importCodexInlineHookSkips(target targetpkg.Target, scope targetpkg.Scope, hookDestination string) ([]adopt.Skipped, error) {
	if target != targetpkg.TargetCodex {
		return nil, nil
	}
	configDestination, ok := commandhook.CodexInlineConfigDestination(hookDestination)
	if !ok {
		return nil, nil
	}
	configPath, err := adopt.ResolveDestination(configDestination)
	if err != nil {
		return nil, err
	}

	var decoded map[string]toml.Primitive
	metadata, err := toml.DecodeFile(configPath, &decoded)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return []adopt.Skipped{{LivePath: configDestination, Reason: "inline_config_malformed"}}, nil
	}
	if metadata.IsDefined("hooks") {
		return []adopt.Skipped{{LivePath: configDestination, Reason: "unsupported_inline_hooks"}}, nil
	}

	return nil, nil
}

func parseImportHooks(content []byte, target targetpkg.Target, scope targetpkg.Scope, livePath string) ([]adopt.Hook, []adopt.Skipped) {
	rawHooks, ok, reason := importHooksProjection(content)
	if !ok {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: reason}}
	}

	eventNames := sortedImportHookEvents(rawHooks)
	hooks := make([]adopt.Hook, 0)
	skipped := make([]adopt.Skipped, 0)
	seenNames := make(map[string]struct{})
	for _, event := range eventNames {
		if strings.TrimSpace(event) == "" {
			skipped = append(skipped, adopt.Skipped{LivePath: livePath, Reason: "empty_event"})
			continue
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(rawHooks[event], &groups); err != nil {
			skipped = append(skipped, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, 0, 0, "groups_not_array")})
			continue
		}
		for groupIndex, rawGroup := range groups {
			groupHooks, groupSkipped := importHookGroupCandidates(target, scope, livePath, event, groupIndex, rawGroup, seenNames)
			hooks = append(hooks, groupHooks...)
			skipped = append(skipped, groupSkipped...)
		}
	}
	if len(hooks) == 0 && len(skipped) == 0 {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: "hooks_empty"}}
	}

	return hooks, skipped
}

func importHooksProjection(content []byte) (map[string]json.RawMessage, bool, string) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, false, "empty_json"
	}
	var settings map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&settings); err != nil {
		return nil, false, "malformed_json"
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return nil, false, "multiple_json_values"
	} else if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, "malformed_json"
	}
	if settings == nil {
		return nil, false, "top_level_not_object"
	}
	rawHooks, ok := settings["hooks"]
	if !ok {
		return nil, false, "hooks_missing"
	}
	if bytes.Equal(bytes.TrimSpace(rawHooks), []byte("null")) {
		return nil, false, "hooks_null"
	}
	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(rawHooks, &hooks); err != nil || hooks == nil {
		return nil, false, "hooks_not_object"
	}

	return hooks, true, ""
}

func sortedImportHookEvents(rawHooks map[string]json.RawMessage) []string {
	events := make([]string, 0, len(rawHooks))
	for event := range rawHooks {
		events = append(events, event)
	}
	sort.Strings(events)

	return events
}

func importHookGroupCandidates(
	target targetpkg.Target,
	scope targetpkg.Scope,
	livePath string,
	event string,
	groupIndex int,
	rawGroup json.RawMessage,
	seenNames map[string]struct{},
) ([]adopt.Hook, []adopt.Skipped) {
	if unsupported, ok := unsupportedImportHookField(rawGroup, importHookGroupAllowedFields()); ok {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, 0, "unsupported_group_field_"+unsupported)}}
	}
	var group importHookGroup
	if err := json.Unmarshal(rawGroup, &group); err != nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, 0, "malformed_group")}}
	}
	if group.Hooks == nil {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, 0, "missing_handlers")}}
	}

	hooks := make([]adopt.Hook, 0, len(group.Hooks))
	skipped := make([]adopt.Skipped, 0)
	for handlerIndex, rawHandler := range group.Hooks {
		hook, skip, ok := importHookHandlerCandidate(target, scope, livePath, event, groupIndex, handlerIndex, group.Matcher, rawHandler, seenNames)
		if ok {
			hooks = append(hooks, hook)
		} else {
			skipped = append(skipped, skip)
		}
	}

	return hooks, skipped
}

func importHookHandlerCandidate(
	target targetpkg.Target,
	scope targetpkg.Scope,
	livePath string,
	event string,
	groupIndex int,
	handlerIndex int,
	matcher string,
	rawHandler json.RawMessage,
	seenNames map[string]struct{},
) (adopt.Hook, adopt.Skipped, bool) {
	if unsupported, ok := unsupportedImportHookField(rawHandler, importHookHandlerAllowedFields()); ok {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "unsupported_handler_field_"+unsupported)}, false
	}
	var handler importHookHandler
	if err := json.Unmarshal(rawHandler, &handler); err != nil {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "malformed_handler")}, false
	}
	if strings.TrimSpace(handler.Type) != declarationHookTypeCommand {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "unsupported_handler_type")}, false
	}
	if strings.TrimSpace(handler.Command) == "" {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "missing_command")}, false
	}
	if handler.Async {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "unsupported_async")}, false
	}
	if target == targetpkg.TargetCodex && strings.TrimSpace(handler.Condition) != "" {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "unsupported_condition")}, false
	}
	if err := commandhook.ValidateShape("import", target, strings.TrimSpace(event), strings.TrimSpace(matcher), strings.TrimSpace(handler.Condition)); err != nil {
		return adopt.Hook{}, adopt.Skipped{LivePath: livePath, Reason: importHookSkipReason(event, groupIndex, handlerIndex, "unsupported_target_shape")}, false
	}

	return adopt.Hook{
		ResourceName:  uniqueImportHookName(target, scope, event, groupIndex, handlerIndex, seenNames),
		Target:        target,
		Scope:         scope,
		LivePath:      livePath,
		Event:         strings.TrimSpace(event),
		Matcher:       strings.TrimSpace(matcher),
		Command:       strings.TrimSpace(handler.Command),
		Timeout:       handler.Timeout,
		StatusMessage: strings.TrimSpace(handler.StatusMessage),
		Condition:     strings.TrimSpace(handler.Condition),
	}, adopt.Skipped{}, true
}

func unsupportedImportHookField(content []byte, allowed map[string]struct{}) (string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		return "", false
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := allowed[name]; !ok {
			return name, true
		}
	}

	return "", false
}

func importHookGroupAllowedFields() map[string]struct{} {
	return map[string]struct{}{
		"matcher": {},
		"hooks":   {},
	}
}

func importHookHandlerAllowedFields() map[string]struct{} {
	return map[string]struct{}{
		"type":          {},
		"command":       {},
		"if":            {},
		"async":         {},
		"timeout":       {},
		"statusMessage": {},
	}
}

func uniqueImportHookName(
	target targetpkg.Target,
	scope targetpkg.Scope,
	event string,
	groupIndex int,
	handlerIndex int,
	seen map[string]struct{},
) string {
	base := sanitizeImportHookName(fmt.Sprintf("%s_%s_%s_%d_%d", target, scope, event, groupIndex+1, handlerIndex+1))
	name := base
	for suffix := 2; ; suffix++ {
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			return name
		}
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
}

func sanitizeImportHookName(value string) string {
	var builder strings.Builder
	lastUnderscore := false
	for _, item := range strings.ToLower(value) {
		if (item >= 'a' && item <= 'z') || (item >= '0' && item <= '9') {
			builder.WriteRune(item)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && builder.Len() != 0 {
			builder.WriteByte('_')
			lastUnderscore = true
		}
	}
	name := strings.Trim(builder.String(), "_")
	if name == "" {
		return "hook"
	}

	return name
}

func importHookSkipReason(event string, groupIndex int, handlerIndex int, reason string) string {
	return fmt.Sprintf("event=%s,group=%d,handler=%d,%s", sanitizeImportHookReason(event), groupIndex+1, handlerIndex+1, reason)
}

func sanitizeImportHookReason(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}

	return strings.ReplaceAll(value, " ", "_")
}
