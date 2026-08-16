package codec

import (
	"sort"
	"strconv"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
)

func renderSkillTargetTables(
	builder *strings.Builder,
	root string,
	targets map[string]declaration.SkillTarget,
) {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		builder.WriteByte('\n')
		builder.WriteByte('[')
		builder.WriteString(root)
		builder.WriteString(".target.")
		builder.WriteString(strconv.Quote(name))
		builder.WriteString("]\n")
		builder.WriteString("install_to = ")
		builder.WriteString(strconv.Quote(targets[name].InstallTo))
		builder.WriteByte('\n')
	}
}

func mergeSkillTargetTables(
	block string,
	root string,
	existing map[string]declaration.SkillTarget,
	incoming map[string]declaration.SkillTarget,
) string {
	additions := make(map[string]declaration.SkillTarget)
	for name, placement := range incoming {
		if _, present := existing[name]; !present {
			additions[name] = placement
		}
	}
	if len(additions) == 0 {
		return block
	}

	var builder strings.Builder
	builder.WriteString(strings.TrimRight(block, "\n"))
	builder.WriteByte('\n')
	renderSkillTargetTables(&builder, root, additions)
	return builder.String()
}

func skillTargetMapsCompatible(
	left map[string]declaration.SkillTarget,
	right map[string]declaration.SkillTarget,
) bool {
	for name, leftPlacement := range left {
		if rightPlacement, shared := right[name]; shared && rightPlacement != leftPlacement {
			return false
		}
	}
	return true
}

// RemoveSkillTargetTables removes target-local placement metadata from one
// skill or skill_group block.
func RemoveSkillTargetTables(block string, root string, selectedTargets []string) string {
	selected := make(map[string]struct{}, len(selectedTargets))
	for _, selectedTarget := range selectedTargets {
		selected[selectedTarget] = struct{}{}
	}

	ranges := make([]declaration.DocumentRange, 0)
	activeStart := -1
	declaration.WalkStructuralLines([]byte(block), func(lineStart int, trimmed string) bool {
		if isSingleTOMLTableHeader(trimmed) {
			if activeStart >= 0 {
				ranges = append(ranges, declaration.DocumentRange{Start: activeStart, End: lineStart})
				activeStart = -1
			}
			if targetName, ok := parseSkillTargetHeader(trimmed, root); ok {
				if _, remove := selected[targetName]; remove {
					activeStart = lineStart
				}
			}
		}
		return false
	})
	if activeStart >= 0 {
		ranges = append(ranges, declaration.DocumentRange{Start: activeStart, End: len(block)})
	}

	output := []byte(block)
	for index := len(ranges) - 1; index >= 0; index-- {
		output = declaration.RemoveDocumentRange(output, ranges[index])
	}
	return string(output)
}

func parseSkillTargetHeader(line string, root string) (string, bool) {
	header, ok := declaration.ParseTableHeader(line)
	if !ok || header.Array || len(header.Segments) != 3 ||
		header.Segments[0] != root || header.Segments[1] != "target" {
		return "", false
	}
	return header.Segments[2], true
}
