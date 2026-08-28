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
	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/encoding/hookdocument"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/realization/aggregate/hook"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const declarationHookTypeCommand = "command"

const (
	importHookSkipMissing                     = "missing"
	importHookSkipNotRegular                  = "not_regular_file"
	importHookSkipSymlink                     = "hook_final_symlink"
	importHookSkipTooLarge                    = "hook_file_too_large"
	importHookSkipChanged                     = "hook_file_changed_during_read"
	importHookSkipDuplicateJSONKey            = "duplicate_json_key"
	importHookSkipJSONDepth                   = "json_depth_exceeded"
	importHookSkipBudgetExceeded              = "hook_import_budget_exceeded"
	importHookSkipInvalidCanonical            = "invalid_canonical_hook"
	importHookSkipInlineConfigStructure       = "inline_config_structure_limit"
	maximumInlineConfigBytes            int64 = 4 << 20
	maximumImportHookSkips                    = 4096
	maximumImportHookDiagnosticBytes          = 256 << 10
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

type candidateHooks struct {
	readRegularFile func(context.Context, string, int64) ([]byte, bool, error)
}

func Candidates(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	skipped adopt.SkipEmitter,
) ([]adopt.Hook, error) {
	return candidatesWithHooks(ctx, target, scope, skipped, candidateHooks{})
}

func candidatesWithHooks(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	skipped adopt.SkipEmitter,
	hooks candidateHooks,
) ([]adopt.Hook, error) {
	if ctx == nil {
		return nil, fmt.Errorf("hook import context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	readRegularFile := hooks.readRegularFile
	if readRegularFile == nil {
		readRegularFile = filesnapshot.ReadRegularFileContext
	}
	liveDestination, ok := commandhook.Destination(target, scope)
	if !ok {
		if err := skipped.Add(adopt.UnsupportedSurfaceSkip(target, scope, "hooks")); err != nil {
			return nil, err
		}
		return nil, nil
	}
	inlineSkipped, err := importCodexInlineHookSkip(ctx, target, scope, liveDestination, readRegularFile)
	if err != nil {
		return nil, err
	}
	livePath, err := hookDestinationPath(liveDestination, scope)
	if err != nil {
		return nil, err
	}
	liveDestinationValue := liveDestination.String()
	content, exists, err := readRegularFile(
		ctx,
		livePath,
		hookdocument.MaximumBytes,
	)
	if err != nil {
		if skip, ok := hookSnapshotSkip(liveDestinationValue, err); ok {
			if err := skipped.Add(skip); err != nil {
				return nil, err
			}
			if inlineSkipped.Reason != "" {
				if err := skipped.Add(inlineSkipped); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		return nil, fmt.Errorf("read live hook path %q: %w", liveDestinationValue, err)
	}
	if !exists {
		if err := skipped.Add(adopt.Skipped{LivePath: liveDestinationValue, Reason: importHookSkipMissing}); err != nil {
			return nil, err
		}
		if inlineSkipped.Reason != "" {
			if err := skipped.Add(inlineSkipped); err != nil {
				return nil, err
			}
		}
		return nil, nil
	}

	importedHooks, documentSkipped := parseImportHooks(content, target, scope, liveDestinationValue)
	if err := emitHookSkips(ctx, skipped, documentSkipped); err != nil {
		return nil, err
	}
	if inlineSkipped.Reason != "" {
		if err := skipped.Add(inlineSkipped); err != nil {
			return nil, err
		}
	}
	return importedHooks, nil
}

func importCodexInlineHookSkip(
	ctx context.Context,
	target targetpkg.Target,
	scope targetpkg.Scope,
	hookDestination output.Destination,
	readRegularFile func(context.Context, string, int64) ([]byte, bool, error),
) (adopt.Skipped, error) {
	if target != targetpkg.TargetCodex {
		return adopt.Skipped{}, nil
	}
	configDestination, ok := commandhook.CodexInlineConfigDestination(hookDestination)
	if !ok {
		return adopt.Skipped{}, nil
	}
	configPath, err := hookDestinationPath(configDestination, scope)
	if err != nil {
		return adopt.Skipped{}, err
	}

	content, exists, err := readRegularFile(ctx, configPath, maximumInlineConfigBytes)
	if err != nil {
		if skip, ok := hookSnapshotSkip(configDestination.String(), err); ok {
			return skip, nil
		}
		return adopt.Skipped{}, fmt.Errorf("read Codex inline hook config %q: %w", configDestination, err)
	}
	if !exists {
		return adopt.Skipped{}, nil
	}
	if err := tomlstrict.Admit(ctx, content, tomlstrict.StandardLimits()); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return adopt.Skipped{}, err
		}
		reason := adopt.SkipReason(importHookSkipInlineConfigStructure)
		if errors.Is(err, tomlstrict.ErrMalformed) {
			reason = "inline_config_malformed"
		}
		return adopt.Skipped{LivePath: configDestination.String(), Reason: reason}, nil
	}
	var decoded map[string]toml.Primitive
	metadata, err := tomlstrict.DecodeAdmitted(ctx, content, &decoded)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return adopt.Skipped{}, err
		}
		return adopt.Skipped{LivePath: configDestination.String(), Reason: "inline_config_malformed"}, nil
	}
	if metadata.IsDefined("hooks") {
		return adopt.Skipped{LivePath: configDestination.String(), Reason: "unsupported_inline_hooks"}, nil
	}

	return adopt.Skipped{}, nil
}

func emitHookSkips(ctx context.Context, skipped adopt.SkipEmitter, values []adopt.Skipped) error {
	for _, value := range values {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := skipped.Add(value); err != nil {
			return err
		}
	}
	return ctx.Err()
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
			collector.addSkip("empty_event", "")
			continue
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(rawHooks[event], &groups); err != nil {
			collector.addSkip("groups_not_array", importHookSkipDetail(identity.diagnosticToken, 0, 0, ""))
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

func importHooksProjection(content []byte) (map[string]json.RawMessage, bool, adopt.SkipReason) {
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, false, "empty_json"
	}
	if int64(len(content)) > hookdocument.MaximumBytes {
		return nil, false, importHookSkipTooLarge
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
	var reason adopt.SkipReason
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

func hookSyntaxSkipReason(err error) adopt.SkipReason {
	switch {
	case errors.Is(err, jsonstrict.ErrDuplicateObjectKey):
		return importHookSkipDuplicateJSONKey
	case errors.Is(err, jsonstrict.ErrMaximumDepthExceeded):
		return importHookSkipJSONDepth
	case errors.Is(err, hookdocument.ErrStructuralBudgetExceeded):
		return importHookSkipBudgetExceeded
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
		collector.addSkip(
			"unsupported_group_field",
			importHookSkipDetail(identity.diagnosticToken, groupIndex, 0, boundedImportHookToken(unsupported)),
		)
		return
	}
	var group importHookGroup
	if err := json.Unmarshal(rawGroup, &group); err != nil {
		collector.addSkip("malformed_group", importHookSkipDetail(identity.diagnosticToken, groupIndex, 0, ""))
		return
	}
	if group.Hooks == nil {
		collector.addSkip("missing_handlers", importHookSkipDetail(identity.diagnosticToken, groupIndex, 0, ""))
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
		collector.addSkip(
			"unsupported_handler_field",
			importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, boundedImportHookToken(unsupported)),
		)
		return
	}
	var handler importHookHandler
	if err := json.Unmarshal(rawHandler, &handler); err != nil {
		collector.addSkip("malformed_handler", importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
		return
	}
	if strings.TrimSpace(handler.Type) != declarationHookTypeCommand {
		collector.addSkip("unsupported_handler_type", importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
		return
	}
	if strings.TrimSpace(handler.Command) == "" {
		collector.addSkip("missing_command", importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
		return
	}
	if handler.Async {
		collector.addSkip("unsupported_async", importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
		return
	}
	if collector.target == targetpkg.TargetCodex && strings.TrimSpace(handler.Condition) != "" {
		collector.addSkip("unsupported_condition", importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
		return
	}
	if err := commandhook.ValidateShape("import", collector.target, strings.TrimSpace(event), strings.TrimSpace(matcher), strings.TrimSpace(handler.Condition)); err != nil {
		collector.addSkip("unsupported_target_shape", importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
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
		collector.addSkip(importHookSkipInvalidCanonical, importHookSkipDetail(identity.diagnosticToken, groupIndex, handlerIndex, ""))
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
