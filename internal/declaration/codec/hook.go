package codec

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

type HookBlock struct {
	Start int
	End   int
	Hook  declaration.Hook
}

func ScanHookBlocks(content []byte) ([]HookBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return declaration.StartsArrayTableRoot(trimmed, "hook") },
		startsNewHookTable,
	)
	blocks := make([]HookBlock, 0)
	for _, targetRange := range ranges {
		block, err := parseHookBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func startsNewHookTable(trimmedLine string) bool {
	return declaration.StartsTableOutsideRoot(trimmedLine, "hook")
}

func parseHookBlock(content []byte, start int, end int) (HookBlock, error) {
	var decoded struct {
		Hooks []declaration.Hook `toml:"hook"`
	}
	if _, err := toml.Decode(string(content[start:end]), &decoded); err != nil {
		return HookBlock{}, fmt.Errorf("parse existing hook block: %w", err)
	}
	if len(decoded.Hooks) != 1 {
		return HookBlock{}, fmt.Errorf("parse existing hook block: expected one hook")
	}
	return HookBlock{
		Start: start,
		End:   end,
		Hook:  decoded.Hooks[0],
	}, nil
}

// sameHookIdentity reports whether two hook declarations may share one block while
// differing only in their explicit target sets and target overrides.
func sameHookIdentity(left declaration.Hook, right declaration.Hook) bool {
	return strings.TrimSpace(left.Event) == strings.TrimSpace(right.Event) &&
		strings.TrimSpace(left.Matcher) == strings.TrimSpace(right.Matcher) &&
		effectiveHookType(left.Type) == effectiveHookType(right.Type) &&
		strings.TrimSpace(left.Command) == strings.TrimSpace(right.Command) &&
		left.TimeoutSeconds == right.TimeoutSeconds &&
		strings.TrimSpace(left.StatusMessage) == strings.TrimSpace(right.StatusMessage) &&
		strings.TrimSpace(left.Scope) == strings.TrimSpace(right.Scope)
}

// HookOverridesByTarget indexes declaration-local target overrides.
func HookOverridesByTarget(overrides []declaration.HookTargetOverride) map[string]declaration.HookTargetOverride {
	result := make(map[string]declaration.HookTargetOverride, len(overrides))
	for _, override := range overrides {
		result[override.Target] = override
	}
	return result
}

// FilterHookOverrides retains target overrides associated with the selected
// declaration targets, preserving declaration order.
func FilterHookOverrides(overrides []declaration.HookTargetOverride, targets []string) []declaration.HookTargetOverride {
	targetSet := make(map[string]struct{}, len(targets))
	for _, selectedTarget := range targets {
		targetSet[selectedTarget] = struct{}{}
	}
	result := make([]declaration.HookTargetOverride, 0, len(overrides))
	for _, override := range overrides {
		if _, ok := targetSet[override.Target]; ok {
			result = append(result, override)
		}
	}
	return result
}

// ApplyHookAdd appends a hook declaration or merges its explicit target set while
// preserving unrelated manifest bytes. The caller retains target admission
// policy through mergeTargets.
func ApplyHookAdd(
	original []byte,
	header declaration.ManifestHeader,
	hook declaration.Hook,
	mergeTargets func(existing declaration.Hook, incoming declaration.Hook, mergedTargets []string, header declaration.ManifestHeader) (declaration.Hook, error),
) (declaration.EditResult, error) {
	if mergeTargets == nil {
		return declaration.EditResult{}, fmt.Errorf("hook target merge policy is required")
	}
	return declaration.ApplyAddDeclaration(declaration.AddEditInput[declaration.Hook]{
		Original:    original,
		Header:      header,
		Declaration: hook,
		Codec: declaration.AddEditContract[declaration.Hook]{
			Kind: declaration.KindHook,
			Scan: scanHookEditBlocks,
			Key: func(value declaration.Hook) (declaration.Key, error) {
				return declaration.Key{Kind: declaration.KindHook, Name: value.Name}, nil
			},
			ExplicitTargets: func(value declaration.Hook) declaration.Targets {
				return declaration.Targets(value.Targets)
			},
			SameIdentity: func(existing declaration.Hook, incoming declaration.Hook, _ declaration.ManifestHeader) bool {
				return sameHookIdentity(existing, incoming)
			},
			RenderBlock: RenderHookBlock,
			RenderBlockWithTargets: func(originalBlock string, existing declaration.Hook, incoming declaration.Hook, mergedTargetsValue declaration.Targets, header declaration.ManifestHeader) (string, error) {
				merged, err := mergeTargets(existing, incoming, mergedTargetsValue.Values(), header)
				if err != nil {
					return "", err
				}
				return UpdateHookTargets(originalBlock, existing, merged)
			},
			DuplicateError: func(key declaration.Key) error {
				return fmt.Errorf("duplicate hook name %q", key.Name)
			},
			AlreadyExistsError: func(key declaration.Key) error {
				return fmt.Errorf("hook %q already exists", key.Name)
			},
			InheritsTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("hook %q inherits manifest targets; edit the manifest manually to change target inheritance", key.Name)
			},
			AlreadyHasTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("hook %q already has the selected targets", key.Name)
			},
		},
	})
}

func scanHookEditBlocks(content []byte) ([]declaration.EditBlock[declaration.Hook], error) {
	blocks, err := ScanHookBlocks(content)
	if err != nil {
		return nil, err
	}
	declarations := make([]declaration.EditBlock[declaration.Hook], 0, len(blocks))
	for _, block := range blocks {
		declarations = append(declarations, declaration.EditBlock[declaration.Hook]{
			Range: declaration.DocumentRange{Start: block.Start, End: block.End},
			Value: block.Hook,
		})
	}
	return declarations, nil
}

func effectiveHookType(value string) string {
	hookType := strings.TrimSpace(value)
	if hookType == "" {
		return "command"
	}
	return hookType
}

// UpdateHookTargets changes the root targets assignment and target override
// tables whose target membership changed. An empty updated target set removes
// the local assignment so the hook can inherit manifest targets.
func UpdateHookTargets(block string, existing declaration.Hook, updated declaration.Hook) (string, error) {
	ranges := hookTargetOverrideRanges(block)
	if len(ranges) != len(existing.TargetOverrides) {
		return "", fmt.Errorf(
			"update hook targets: found %d target_override tables for %d decoded overrides",
			len(ranges),
			len(existing.TargetOverrides),
		)
	}

	updatedByTarget := HookOverridesByTarget(updated.TargetOverrides)
	content := []byte(block)
	for index := len(ranges) - 1; index >= 0; index-- {
		if _, retained := updatedByTarget[existing.TargetOverrides[index].Target]; retained {
			continue
		}
		content = declaration.RemoveDocumentRange(content, ranges[index])
	}

	rewritten := string(content)
	if len(updated.Targets) == 0 {
		removed, ok, err := declaration.RemoveRootAssignment(rewritten, "targets")
		if err != nil {
			return "", fmt.Errorf("update hook targets: %w", err)
		}
		if ok {
			rewritten = removed
		}
	} else {
		replaced, ok, err := declaration.ReplaceRootAssignment(rewritten, "targets", renderStringArray(updated.Targets))
		if err != nil {
			return "", fmt.Errorf("update hook targets: %w", err)
		}
		if ok {
			rewritten = replaced
		} else {
			inserted, insertErr := declaration.InsertRootAssignment(rewritten, "targets", renderStringArray(updated.Targets))
			if insertErr != nil {
				return "", fmt.Errorf("update hook targets: %w", insertErr)
			}
			rewritten = inserted
		}
	}

	existingByTarget := HookOverridesByTarget(existing.TargetOverrides)
	added := make([]declaration.HookTargetOverride, 0)
	for _, override := range updated.TargetOverrides {
		if _, exists := existingByTarget[override.Target]; !exists {
			added = append(added, override)
		}
	}
	return appendHookTargetOverrides(rewritten, added, strings.HasSuffix(block, "\n")), nil
}

func hookTargetOverrideRanges(block string) []declaration.DocumentRange {
	lines := bytes.SplitAfter([]byte(block), []byte("\n"))
	ranges := make([]declaration.DocumentRange, 0)
	offset := 0
	activeStart := -1
	for _, line := range lines {
		header, isHeader := declaration.ParseTableHeader(strings.TrimSpace(string(line)))
		if isHeader && activeStart >= 0 {
			ranges = append(ranges, declaration.DocumentRange{Start: activeStart, End: offset})
			activeStart = -1
		}
		if isHeader && header.Array &&
			len(header.Segments) == 2 &&
			header.Segments[0] == "hook" &&
			header.Segments[1] == "target_override" {
			activeStart = offset
		}
		offset += len(line)
	}
	if activeStart >= 0 {
		ranges = append(ranges, declaration.DocumentRange{Start: activeStart, End: len(block)})
	}
	return ranges
}

func appendHookTargetOverrides(block string, overrides []declaration.HookTargetOverride, hadTerminalNewline bool) string {
	if len(overrides) == 0 {
		if !hadTerminalNewline {
			return strings.TrimRight(block, "\r\n")
		}
		return block
	}

	lineEnding := "\n"
	if strings.Contains(block, "\r\n") {
		lineEnding = "\r\n"
	}
	output := strings.TrimRight(block, "\r\n")
	for _, override := range overrides {
		output += lineEnding + lineEnding
		output += renderHookTargetOverride(override, lineEnding)
	}
	if hadTerminalNewline {
		output += lineEnding
	}
	return output
}

func RenderHookBlock(hook declaration.Hook) string {
	var builder strings.Builder
	builder.WriteString("[[hook]]\n")
	builder.WriteString("name = ")
	builder.WriteString(strconv.Quote(hook.Name))
	builder.WriteByte('\n')
	builder.WriteString("event = ")
	builder.WriteString(strconv.Quote(hook.Event))
	builder.WriteByte('\n')
	if hook.Matcher != "" {
		builder.WriteString("matcher = ")
		builder.WriteString(strconv.Quote(hook.Matcher))
		builder.WriteByte('\n')
	}
	if hook.Type != "" {
		builder.WriteString("type = ")
		builder.WriteString(strconv.Quote(hook.Type))
		builder.WriteByte('\n')
	}
	builder.WriteString("command = ")
	builder.WriteString(strconv.Quote(hook.Command))
	builder.WriteByte('\n')
	if hook.TimeoutSeconds != 0 {
		builder.WriteString("timeout = ")
		builder.WriteString(strconv.Itoa(hook.TimeoutSeconds))
		builder.WriteByte('\n')
	}
	if hook.StatusMessage != "" {
		builder.WriteString("status_message = ")
		builder.WriteString(strconv.Quote(hook.StatusMessage))
		builder.WriteByte('\n')
	}
	if len(hook.Targets) != 0 {
		builder.WriteString("targets = ")
		builder.WriteString(renderStringArray(hook.Targets))
		builder.WriteByte('\n')
	}
	if hook.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(hook.Scope))
		builder.WriteByte('\n')
	}
	for _, override := range hook.TargetOverrides {
		builder.WriteByte('\n')
		builder.WriteString(renderHookTargetOverride(override, "\n"))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func renderHookTargetOverride(override declaration.HookTargetOverride, lineEnding string) string {
	var builder strings.Builder
	builder.WriteString("[[hook.target_override]]")
	builder.WriteString(lineEnding)
	builder.WriteString("target = ")
	builder.WriteString(strconv.Quote(override.Target))
	builder.WriteString(lineEnding)
	if override.Matcher != "" {
		builder.WriteString("matcher = ")
		builder.WriteString(strconv.Quote(override.Matcher))
		builder.WriteString(lineEnding)
	}
	if override.Condition != "" {
		builder.WriteString("if = ")
		builder.WriteString(strconv.Quote(override.Condition))
		builder.WriteString(lineEnding)
	}
	return strings.TrimSuffix(builder.String(), lineEnding)
}
