package codec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

type SkillSource struct {
	Git  string `toml:"git"`
	Path string `toml:"path"`
	Ref  string `toml:"ref"`
	Mode string `toml:"mode"`
}

func startsNewSkillTopLevelTable(trimmedLine string) bool {
	return declaration.StartsTableOutsideRoot(trimmedLine, "skill")
}

type Skill struct {
	ID          string      `toml:"id"`
	Name        string      `toml:"name"`
	Source      SkillSource `toml:"source"`
	Targets     []string    `toml:"targets"`
	Scope       string      `toml:"scope"`
	InstallMode string      `toml:"install_mode"`
	Portable    *bool       `toml:"portable"`
}

type SkillBlock struct {
	Start int
	End   int
	Skill Skill
}

func ScanSkillBlocks(content []byte) ([]SkillBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return declaration.StartsArrayTableRoot(trimmed, "skill") },
		startsNewSkillTopLevelTable,
	)
	blocks := make([]SkillBlock, 0)
	for _, targetRange := range ranges {
		block, err := parseSkillBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func parseSkillBlock(content []byte, start int, end int) (SkillBlock, error) {
	var decoded struct {
		Skills []Skill `toml:"skill"`
	}
	if _, err := toml.Decode(string(content[start:end]), &decoded); err != nil {
		return SkillBlock{}, fmt.Errorf("parse existing skill block: %w", err)
	}
	if len(decoded.Skills) != 1 {
		return SkillBlock{}, fmt.Errorf("parse existing skill block: expected one skill")
	}
	return SkillBlock{
		Start: start,
		End:   end,
		Skill: decoded.Skills[0],
	}, nil
}

// ResourceID returns the document-local identity of the skill declaration.
func (skill Skill) ResourceID() string {
	if strings.TrimSpace(skill.ID) != "" {
		return strings.TrimSpace(skill.ID)
	}
	return strings.TrimSpace(skill.Name)
}

// sameSkillIdentity reports whether two skill declarations may share one block
// while differing only in their explicit target sets.
func sameSkillIdentity(left Skill, right Skill) bool {
	return strings.TrimSpace(left.Name) == strings.TrimSpace(right.Name) &&
		strings.TrimSpace(left.Scope) == strings.TrimSpace(right.Scope) &&
		left.Source == right.Source
}

// ApplySkillAdd appends a skill declaration or merges its explicit target set while
// preserving unrelated manifest bytes.
func ApplySkillAdd(original []byte, skill Skill) (declaration.EditResult, error) {
	return declaration.ApplyAddDeclaration(declaration.AddEditInput[Skill]{
		Original:    original,
		Declaration: skill,
		Codec: declaration.AddEditContract[Skill]{
			Kind: declaration.KindSkill,
			Scan: scanSkillEditBlocks,
			Key: func(value Skill) (declaration.Key, error) {
				return declaration.Key{Kind: declaration.KindSkill, Name: value.ResourceID()}, nil
			},
			ExplicitTargets: func(value Skill) declaration.Targets {
				return declaration.Targets(value.Targets)
			},
			SameIdentity: func(existing Skill, incoming Skill, _ declaration.ManifestHeader) bool {
				return sameSkillIdentity(existing, incoming)
			},
			RenderBlock: RenderSkillBlock,
			RenderBlockWithTargets: func(originalBlock string, _ Skill, _ Skill, mergedTargets declaration.Targets, _ declaration.ManifestHeader) (string, error) {
				return ReplaceSkillTargets(originalBlock, mergedTargets.Values()), nil
			},
			DuplicateError: func(key declaration.Key) error {
				return fmt.Errorf("duplicate skill id %q", key.Name)
			},
			AlreadyExistsError: func(key declaration.Key) error {
				return fmt.Errorf("skill %q already exists", key.Name)
			},
			InheritsTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("skill %q inherits manifest targets; edit the manifest manually to change target inheritance", key.Name)
			},
			AlreadyHasTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("skill %q already has the selected targets", key.Name)
			},
		},
	})
}

func scanSkillEditBlocks(content []byte) ([]declaration.EditBlock[Skill], error) {
	blocks, err := ScanSkillBlocks(content)
	if err != nil {
		return nil, err
	}
	declarations := make([]declaration.EditBlock[Skill], 0, len(blocks))
	for _, block := range blocks {
		declarations = append(declarations, declaration.EditBlock[Skill]{
			Range: declaration.DocumentRange{Start: block.Start, End: block.End},
			Value: block.Skill,
		})
	}
	return declarations, nil
}

func ReplaceSkillTargets(block string, targets []string) string {
	if updated, ok := declaration.ReplaceDocumentAssignmentLine(block, "targets", renderStringArray(targets)); ok {
		return updated
	}
	lines := strings.SplitAfter(block, "\n")
	if len(lines) == 0 {
		return "targets = " + renderStringArray(targets) + "\n"
	}
	insertAt := len(lines)
	if lines[len(lines)-1] == "" {
		insertAt = len(lines) - 1
	}
	newLines := append([]string{}, lines[:insertAt]...)
	newLines = append(newLines, "targets = "+renderStringArray(targets)+"\n")
	newLines = append(newLines, lines[insertAt:]...)
	return strings.Join(newLines, "")
}

func RenderSkillBlock(skill Skill) string {
	var builder strings.Builder
	builder.WriteString("[[skill]]\n")
	if skill.ID != "" {
		builder.WriteString("id = ")
		builder.WriteString(strconv.Quote(skill.ID))
		builder.WriteByte('\n')
	}
	builder.WriteString("name = ")
	builder.WriteString(strconv.Quote(skill.Name))
	builder.WriteByte('\n')
	builder.WriteString("source = ")
	builder.WriteString(renderSkillSource(skill.Source))
	builder.WriteByte('\n')
	if len(skill.Targets) != 0 {
		builder.WriteString("targets = ")
		builder.WriteString(renderStringArray(skill.Targets))
		builder.WriteByte('\n')
	}
	if skill.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(skill.Scope))
		builder.WriteByte('\n')
	}
	if skill.Portable != nil {
		builder.WriteString("portable = ")
		builder.WriteString(strconv.FormatBool(*skill.Portable))
		builder.WriteByte('\n')
	}
	return builder.String()
}

func renderSkillSource(source SkillSource) string {
	if source.Git != "" {
		return "{ git = " + strconv.Quote(source.Git) +
			", path = " + strconv.Quote(source.Path) +
			", ref = " + strconv.Quote(source.Ref) + " }"
	}
	return "{ path = " + strconv.Quote(source.Path) + ", mode = " + strconv.Quote(source.Mode) + " }"
}
