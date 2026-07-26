package codec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

type Hook struct {
	Name            string               `toml:"name"`
	Event           string               `toml:"event"`
	Matcher         string               `toml:"matcher"`
	Type            string               `toml:"type"`
	Command         string               `toml:"command"`
	TimeoutSeconds  int                  `toml:"timeout"`
	StatusMessage   string               `toml:"status_message"`
	Targets         []string             `toml:"targets"`
	Scope           string               `toml:"scope"`
	TargetOverrides []HookTargetOverride `toml:"target_override"`
}

type HookTargetOverride struct {
	Target    string `toml:"target"`
	Condition string `toml:"if"`
	Matcher   string `toml:"matcher"`
}

type HookBlock struct {
	Start int
	End   int
	Hook  Hook
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
		Hooks []Hook `toml:"hook"`
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
func sameHookIdentity(left Hook, right Hook) bool {
	return strings.TrimSpace(left.Event) == strings.TrimSpace(right.Event) &&
		strings.TrimSpace(left.Matcher) == strings.TrimSpace(right.Matcher) &&
		effectiveHookType(left.Type) == effectiveHookType(right.Type) &&
		strings.TrimSpace(left.Command) == strings.TrimSpace(right.Command) &&
		left.TimeoutSeconds == right.TimeoutSeconds &&
		strings.TrimSpace(left.StatusMessage) == strings.TrimSpace(right.StatusMessage) &&
		strings.TrimSpace(left.Scope) == strings.TrimSpace(right.Scope)
}

// HookOverridesByTarget indexes declaration-local target overrides.
func HookOverridesByTarget(overrides []HookTargetOverride) map[string]HookTargetOverride {
	result := make(map[string]HookTargetOverride, len(overrides))
	for _, override := range overrides {
		result[override.Target] = override
	}
	return result
}

// FilterHookOverrides retains target overrides associated with the selected
// declaration targets, preserving declaration order.
func FilterHookOverrides(overrides []HookTargetOverride, targets []string) []HookTargetOverride {
	targetSet := make(map[string]struct{}, len(targets))
	for _, selectedTarget := range targets {
		targetSet[selectedTarget] = struct{}{}
	}
	result := make([]HookTargetOverride, 0, len(overrides))
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
	hook Hook,
	mergeTargets func(existing Hook, incoming Hook, mergedTargets []string, header declaration.ManifestHeader) (Hook, error),
) (declaration.EditResult, error) {
	if mergeTargets == nil {
		return declaration.EditResult{}, fmt.Errorf("hook target merge policy is required")
	}
	return declaration.ApplyAddDeclaration(declaration.AddEditInput[Hook]{
		Original:    original,
		Header:      header,
		Declaration: hook,
		Codec: declaration.AddEditContract[Hook]{
			Kind: declaration.KindHook,
			Scan: scanHookEditBlocks,
			Key: func(value Hook) (declaration.Key, error) {
				return declaration.Key{Kind: declaration.KindHook, Name: value.Name}, nil
			},
			ExplicitTargets: func(value Hook) declaration.Targets {
				return declaration.Targets(value.Targets)
			},
			SameIdentity: func(existing Hook, incoming Hook, _ declaration.ManifestHeader) bool {
				return sameHookIdentity(existing, incoming)
			},
			RenderBlock: RenderHookBlock,
			RenderBlockWithTargets: func(_ string, existing Hook, incoming Hook, mergedTargetsValue declaration.Targets, header declaration.ManifestHeader) (string, error) {
				merged, err := mergeTargets(existing, incoming, mergedTargetsValue.Values(), header)
				if err != nil {
					return "", err
				}
				return RenderHookBlock(merged), nil
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

func scanHookEditBlocks(content []byte) ([]declaration.EditBlock[Hook], error) {
	blocks, err := ScanHookBlocks(content)
	if err != nil {
		return nil, err
	}
	declarations := make([]declaration.EditBlock[Hook], 0, len(blocks))
	for _, block := range blocks {
		declarations = append(declarations, declaration.EditBlock[Hook]{
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

func ReplaceHookBlock(original []byte, block HookBlock, hook Hook) []byte {
	return declaration.ReplaceDocumentRange(
		original,
		declaration.DocumentRange{Start: block.Start, End: block.End},
		[]byte(RenderHookBlock(hook)),
	)
}

func RenderHookBlock(hook Hook) string {
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
		builder.WriteString(renderHookStringArray(hook.Targets))
		builder.WriteByte('\n')
	}
	if hook.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(hook.Scope))
		builder.WriteByte('\n')
	}
	for _, override := range hook.TargetOverrides {
		builder.WriteByte('\n')
		builder.WriteString("[[hook.target_override]]\n")
		builder.WriteString("target = ")
		builder.WriteString(strconv.Quote(override.Target))
		builder.WriteByte('\n')
		if override.Matcher != "" {
			builder.WriteString("matcher = ")
			builder.WriteString(strconv.Quote(override.Matcher))
			builder.WriteByte('\n')
		}
		if override.Condition != "" {
			builder.WriteString("if = ")
			builder.WriteString(strconv.Quote(override.Condition))
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func renderHookStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
