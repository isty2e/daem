package codec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

type SkillGroup struct {
	Names       []string                           `toml:"names"`
	Include     []string                           `toml:"include"`
	Exclude     []string                           `toml:"exclude"`
	Source      SkillSource                        `toml:"source"`
	Targets     []string                           `toml:"targets"`
	Scope       string                             `toml:"scope"`
	InstallMode string                             `toml:"install_mode"`
	Portable    *bool                              `toml:"portable"`
	Target      map[string]declaration.SkillTarget `toml:"target"`
}

type SkillGroupBlock struct {
	Start int
	End   int
	Group SkillGroup
}

func ScanSkillGroupBlocks(content []byte) ([]SkillGroupBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return declaration.StartsArrayTableRoot(trimmed, "skill_group") },
		startsNewSkillGroupTopLevelTable,
	)
	blocks := make([]SkillGroupBlock, 0)
	for _, targetRange := range ranges {
		block, err := parseSkillGroupBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// SkillGroupMembership returns the declaration-local skill_group index for every named
// member. Later duplicate members retain last-declaration-wins behavior.
func SkillGroupMembership(content []byte) (map[string]string, error) {
	blocks, err := ScanSkillGroupBlocks(content)
	if err != nil {
		return nil, err
	}
	membership := make(map[string]string)
	for groupIndex, block := range blocks {
		groupName := fmt.Sprintf("skill_group[%d]", groupIndex)
		for _, name := range block.Group.Names {
			membership[name] = groupName
		}
	}
	return membership, nil
}

func parseSkillGroupBlock(content []byte, start int, end int) (SkillGroupBlock, error) {
	var decoded struct {
		Groups []SkillGroup `toml:"skill_group"`
	}
	if _, err := toml.Decode(string(content[start:end]), &decoded); err != nil {
		return SkillGroupBlock{}, fmt.Errorf("parse existing skill_group block: %w", err)
	}
	if len(decoded.Groups) != 1 {
		return SkillGroupBlock{}, fmt.Errorf("parse existing skill_group block: expected one skill_group")
	}
	return SkillGroupBlock{
		Start: start,
		End:   end,
		Group: decoded.Groups[0],
	}, nil
}

func startsNewSkillGroupTopLevelTable(trimmedLine string) bool {
	return declaration.StartsTableOutsideRoot(trimmedLine, "skill_group")
}

func ReplaceSkillGroupNames(block string, names []string) string {
	if updated, ok := declaration.ReplaceDocumentAssignmentLine(block, "names", renderStringArray(names)); ok {
		return updated
	}
	return block
}

// ReplaceSkillGroupTargets rewrites or inserts the root target assignment
// before any target-local placement tables.
func ReplaceSkillGroupTargets(block string, targets []string) string {
	return replaceSkillTargets(block, "skill_group", targets)
}

func RenderSkillGroupBlock(group SkillGroup) string {
	var builder strings.Builder
	builder.WriteString("[[skill_group]]\n")
	if len(group.Include) != 0 {
		builder.WriteString("include = ")
		builder.WriteString(renderStringArray(group.Include))
		builder.WriteByte('\n')
		if len(group.Exclude) != 0 {
			builder.WriteString("exclude = ")
			builder.WriteString(renderStringArray(group.Exclude))
			builder.WriteByte('\n')
		}
	} else {
		builder.WriteString("names = ")
		builder.WriteString(renderStringArray(group.Names))
		builder.WriteByte('\n')
	}
	builder.WriteString("source = ")
	builder.WriteString(renderSkillSource(group.Source))
	builder.WriteByte('\n')
	if len(group.Targets) != 0 {
		builder.WriteString("targets = ")
		builder.WriteString(renderStringArray(group.Targets))
		builder.WriteByte('\n')
	}
	if group.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(group.Scope))
		builder.WriteByte('\n')
	}
	if group.Portable != nil {
		builder.WriteString("portable = ")
		builder.WriteString(strconv.FormatBool(*group.Portable))
		builder.WriteByte('\n')
	}
	renderSkillTargetTables(&builder, "skill_group", group.Target)
	return builder.String()
}
