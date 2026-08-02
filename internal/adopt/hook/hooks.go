package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/assurance/observe/filesnapshot"
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const declarationHookTypeCommand = "command"

const (
	importHookSkipMissing                  = "missing"
	importHookSkipNotRegular               = "not_regular_file"
	importHookSkipSymlink                  = "hook_final_symlink"
	importHookSkipTooLarge                 = "hook_file_too_large"
	importHookSkipChanged                  = "hook_file_changed_during_read"
	importHookSkipDuplicateJSONKey         = "duplicate_json_key"
	importHookSkipJSONDepth                = "json_depth_exceeded"
	importHookSkipBudgetExceeded           = "hook_import_budget_exceeded"
	importHookSkipInvalidCanonical         = "invalid_canonical_hook"
	maximumInlineConfigBytes         int64 = 4 << 20
	maximumImportHookEventBytes            = 256
	maximumImportHookEvents                = 256
	maximumImportHookGroups                = 4096
	maximumImportHookHandlers              = 4096
	maximumImportHookSkips                 = 4096
	maximumImportHookDiagnosticBytes       = 256 << 10
)

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

func Candidates(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
) ([]adopt.Hook, []adopt.Skipped, error) {
	if ctx == nil {
		return nil, nil, fmt.Errorf("hook import context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	liveDestination, ok := commandhook.Destination(target, scope)
	if !ok {
		return nil, []adopt.Skipped{adopt.UnsupportedSurfaceSkip(target, scope, "hooks")}, nil
	}
	inlineSkipped, err := importCodexInlineHookSkips(ctx, target, scope, liveDestination)
	if err != nil {
		return nil, nil, err
	}
	livePath, err := hookDestinationPath(liveDestination, scope)
	if err != nil {
		return nil, nil, err
	}
	liveDestinationValue := liveDestination.String()
	content, exists, err := filesnapshot.ReadRegularFileContext(
		ctx,
		livePath,
		hookdocument.MaximumBytes,
	)
	if err != nil {
		if skip, ok := hookSnapshotSkip(liveDestinationValue, err); ok {
			skipped := append([]adopt.Skipped{skip}, inlineSkipped...)
			return nil, skipped, nil
		}
		return nil, nil, fmt.Errorf("read live hook path %q: %w", liveDestinationValue, err)
	}
	if !exists {
		skipped := append([]adopt.Skipped{{LivePath: liveDestinationValue, Reason: importHookSkipMissing}}, inlineSkipped...)
		return nil, skipped, nil
	}

	hooks, skipped := parseImportHooks(content, target, scope, liveDestinationValue)
	skipped = append(skipped, inlineSkipped...)
	return hooks, skipped, nil
}

func importCodexInlineHookSkips(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	hookDestination output.Destination,
) ([]adopt.Skipped, error) {
	if target != targetpkg.TargetCodex {
		return nil, nil
	}
	configDestination, ok := commandhook.CodexInlineConfigDestination(hookDestination)
	if !ok {
		return nil, nil
	}
	configPath, err := hookDestinationPath(configDestination, scope)
	if err != nil {
		return nil, err
	}

	content, exists, err := filesnapshot.ReadRegularFileContext(ctx, configPath, maximumInlineConfigBytes)
	if err != nil {
		if _, ok := hookSnapshotSkip(configDestination.String(), err); ok {
			return []adopt.Skipped{{LivePath: configDestination.String(), Reason: "inline_config_unreadable"}}, nil
		}
		return nil, fmt.Errorf("read Codex inline hook config %q: %w", configDestination, err)
	}
	if !exists {
		return nil, nil
	}
	var decoded map[string]toml.Primitive
	metadata, err := toml.Decode(string(content), &decoded)
	if err != nil {
		return []adopt.Skipped{{LivePath: configDestination.String(), Reason: "inline_config_malformed"}}, nil
	}
	if metadata.IsDefined("hooks") {
		return []adopt.Skipped{{LivePath: configDestination.String(), Reason: "unsupported_inline_hooks"}}, nil
	}

	return nil, nil
}

func hookDestinationPath(destination output.Destination, scope targetpkg.Scope) (string, error) {
	if err := destination.ValidateScope(scope); err != nil {
		return "", fmt.Errorf("validate live hook destination %q: %w", destination, err)
	}
	if destination.RootRole() == output.RootProject {
		return filepath.FromSlash(destination.RelativePath()), nil
	}
	livePath, err := hostpath.NewResolver("").Resolve(destination)
	if err != nil {
		return "", fmt.Errorf("resolve live hook destination %q: %w", destination, err)
	}
	return livePath, nil
}

func parseImportHooks(content []byte, target targetpkg.Target, scope targetpkg.Scope, livePath string) ([]adopt.Hook, []adopt.Skipped) {
	rawHooks, ok, reason := importHooksProjection(content)
	if !ok {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: reason}}
	}

	eventNames := sortedImportHookEvents(rawHooks)
	collector := importHookCollector{target: target, scope: scope, livePath: livePath}
	for _, event := range eventNames {
		identity := newImportHookEventIdentity(event)
		if strings.TrimSpace(event) == "" {
			collector.addSkip("empty_event")
			continue
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(rawHooks[event], &groups); err != nil {
			collector.addSkip(importHookSkipReason(identity.diagnosticToken, 0, 0, "groups_not_array"))
			continue
		}
		for groupIndex, rawGroup := range groups {
			collector.importGroup(event, identity, groupIndex, rawGroup)
			if collector.exceeded {
				break
			}
		}
	}
	if collector.exceeded {
		return importHookBudgetFailure(livePath)
	}
	if len(collector.hooks) == 0 && len(collector.skipped) == 0 {
		return nil, []adopt.Skipped{{LivePath: livePath, Reason: "hooks_empty"}}
	}

	return collector.hooks, collector.skipped
}

func importHooksProjection(content []byte) (map[string]json.RawMessage, bool, string) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, false, "empty_json"
	}
	if int64(len(content)) > hookdocument.MaximumBytes {
		return nil, false, importHookSkipTooLarge
	}
	if err := scanImportHookStructuralBudget(bytes.NewReader(content)); err != nil {
		if errors.Is(err, errImportHookStructuralBudgetExceeded) {
			return nil, false, importHookSkipBudgetExceeded
		}
		return nil, false, "malformed_json"
	}
	if err := hookdocument.Validate(content); err != nil {
		return nil, false, hookSyntaxSkipReason(err)
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

func hookSnapshotSkip(livePath string, err error) (adopt.Skipped, bool) {
	reason := ""
	switch {
	case errors.Is(err, filesnapshot.ErrSymlink):
		reason = importHookSkipSymlink
	case errors.Is(err, filesnapshot.ErrNotRegular):
		reason = importHookSkipNotRegular
	case errors.Is(err, filesnapshot.ErrLimitExceeded):
		reason = importHookSkipTooLarge
	case errors.Is(err, filesnapshot.ErrChanged):
		reason = importHookSkipChanged
	default:
		return adopt.Skipped{}, false
	}
	return adopt.Skipped{LivePath: livePath, Reason: reason}, true
}

func hookSyntaxSkipReason(err error) string {
	switch {
	case errors.Is(err, jsonstrict.ErrDuplicateObjectKey):
		return importHookSkipDuplicateJSONKey
	case errors.Is(err, jsonstrict.ErrMaximumDepthExceeded):
		return importHookSkipJSONDepth
	case errors.Is(err, jsonstrict.ErrMultipleValues):
		return "multiple_json_values"
	case errors.Is(err, hookdocument.ErrTooLarge):
		return importHookSkipTooLarge
	default:
		return "malformed_json"
	}
}

func sortedImportHookEvents(rawHooks map[string]json.RawMessage) []string {
	events := make([]string, 0, len(rawHooks))
	for event := range rawHooks {
		events = append(events, event)
	}
	sort.Strings(events)

	return events
}

func (collector *importHookCollector) importGroup(
	event string,
	identity importHookEventIdentity,
	groupIndex int,
	rawGroup json.RawMessage,
) {
	if unsupported, ok := unsupportedImportHookField(rawGroup, importHookGroupAllowedFields()); ok {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, 0, "unsupported_group_field_"+boundedImportHookToken(unsupported)))
		return
	}
	var group importHookGroup
	if err := json.Unmarshal(rawGroup, &group); err != nil {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, 0, "malformed_group"))
		return
	}
	if group.Hooks == nil {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, 0, "missing_handlers"))
		return
	}
	for handlerIndex, rawHandler := range group.Hooks {
		collector.importHandler(event, identity, groupIndex, handlerIndex, group.Matcher, rawHandler)
		if collector.exceeded {
			return
		}
	}
}

func (collector *importHookCollector) importHandler(
	event string,
	identity importHookEventIdentity,
	groupIndex int,
	handlerIndex int,
	matcher string,
	rawHandler json.RawMessage,
) {
	if unsupported, ok := unsupportedImportHookField(rawHandler, importHookHandlerAllowedFields()); ok {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "unsupported_handler_field_"+boundedImportHookToken(unsupported)))
		return
	}
	var handler importHookHandler
	if err := json.Unmarshal(rawHandler, &handler); err != nil {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "malformed_handler"))
		return
	}
	if strings.TrimSpace(handler.Type) != declarationHookTypeCommand {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "unsupported_handler_type"))
		return
	}
	if strings.TrimSpace(handler.Command) == "" {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "missing_command"))
		return
	}
	if handler.Async {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "unsupported_async"))
		return
	}
	if collector.target == targetpkg.TargetCodex && strings.TrimSpace(handler.Condition) != "" {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "unsupported_condition"))
		return
	}
	if err := commandhook.ValidateShape("import", collector.target, strings.TrimSpace(event), strings.TrimSpace(matcher), strings.TrimSpace(handler.Condition)); err != nil {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, "unsupported_target_shape"))
		return
	}

	candidate := adopt.Hook{
		ResourceName: importHookName(
			collector.target,
			collector.scope,
			identity.resourceEvent,
			groupIndex,
			handlerIndex,
		),
		Target:        collector.target,
		Scope:         collector.scope,
		LivePath:      collector.livePath,
		Event:         strings.TrimSpace(event),
		Matcher:       strings.TrimSpace(matcher),
		Command:       strings.TrimSpace(handler.Command),
		Timeout:       handler.Timeout,
		StatusMessage: strings.TrimSpace(handler.StatusMessage),
		Condition:     strings.TrimSpace(handler.Condition),
	}
	if err := validateImportHookCandidate(candidate); err != nil {
		collector.addSkip(importHookSkipReason(identity.diagnosticToken, groupIndex, handlerIndex, importHookSkipInvalidCanonical))
		return
	}
	candidate.ResourceName = collector.reserveHookName(identity, groupIndex, handlerIndex)
	collector.hooks = append(collector.hooks, candidate)
}

func validateImportHookCandidate(candidate adopt.Hook) error {
	overrides := map[targetpkg.Target]desiredhook.TargetOverride(nil)
	if candidate.Condition != "" {
		overrides = map[targetpkg.Target]desiredhook.TargetOverride{
			candidate.Target: desiredhook.NewTargetOverride(candidate.Condition, ""),
		}
	}
	_, err := desiredhook.New(desiredhook.Spec{
		Name:            candidate.ResourceName,
		Event:           candidate.Event,
		Matcher:         candidate.Matcher,
		Type:            desiredhook.TypeCommand,
		Command:         candidate.Command,
		TimeoutSeconds:  candidate.Timeout,
		StatusMessage:   candidate.StatusMessage,
		Targets:         []targetpkg.Target{candidate.Target},
		Scope:           candidate.Scope,
		TargetOverrides: overrides,
	})
	return err
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
