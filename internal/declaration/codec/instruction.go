package codec

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/target"
)

type Instruction struct {
	Source  InstructionSource                        `toml:"source"`
	Targets []string                                 `toml:"targets"`
	Scope   string                                   `toml:"scope"`
	Target  map[string]declaration.InstructionTarget `toml:"target"`
}

type InstructionSource declaration.Source

type InstructionBlock struct {
	Start       int
	End         int
	Name        string
	Instruction Instruction
}

func ScanInstructionBlocks(content []byte) ([]InstructionBlock, error) {
	lines := bytes.SplitAfter(content, []byte("\n"))
	blocks := make([]InstructionBlock, 0)
	offset := 0
	activeStart := -1
	activeName := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		name, topLevel, underInstruction := parseInstructionHeader(trimmed)
		if activeStart >= 0 && startsUnrelatedInstructionTable(trimmed, activeName, name, underInstruction) {
			block, err := parseInstructionBlock(content, activeStart, offset)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
			activeStart = -1
			activeName = ""
		}
		if topLevel {
			activeStart = offset
			activeName = name
		}
		offset += len(line)
	}
	if activeStart >= 0 {
		block, err := parseInstructionBlock(content, activeStart, len(content))
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func startsUnrelatedInstructionTable(trimmedLine string, activeName string, parsedName string, underInstruction bool) bool {
	if _, ok := declaration.ParseTableHeader(trimmedLine); !ok {
		return false
	}
	return !underInstruction || parsedName != activeName
}

func parseInstructionBlock(content []byte, start int, end int) (InstructionBlock, error) {
	var decoded struct {
		Instructions map[string]Instruction `toml:"instructions"`
	}
	if _, err := toml.Decode(string(content[start:end]), &decoded); err != nil {
		return InstructionBlock{}, fmt.Errorf("parse existing instruction block: %w", err)
	}
	if len(decoded.Instructions) != 1 {
		return InstructionBlock{}, fmt.Errorf("parse existing instruction block: expected one instruction")
	}
	for name, instruction := range decoded.Instructions {
		return InstructionBlock{
			Start:       start,
			End:         end,
			Name:        name,
			Instruction: instruction,
		}, nil
	}
	return InstructionBlock{}, fmt.Errorf("parse existing instruction block: expected one instruction")
}

func parseInstructionHeader(trimmedLine string) (string, bool, bool) {
	header, ok := declaration.ParseTableHeader(trimmedLine)
	if !ok || header.Array || len(header.Segments) < 2 || header.Segments[0] != "instructions" {
		return "", false, false
	}
	return header.Segments[1], len(header.Segments) == 2, true
}

func parseInstructionTargetHeader(trimmedLine string) (string, string, bool) {
	header, ok := declaration.ParseTableHeader(trimmedLine)
	if !ok || header.Array || len(header.Segments) != 4 ||
		header.Segments[0] != "instructions" || header.Segments[2] != "target" {
		return "", "", false
	}
	return header.Segments[1], header.Segments[3], true
}

func isSingleTOMLTableHeader(trimmedLine string) bool {
	header, ok := declaration.ParseTableHeader(trimmedLine)
	return ok && !header.Array
}

func (source *InstructionSource) UnmarshalTOML(value any) error {
	decoded, err := declaration.SourceFromTOMLValue(value)
	if err != nil {
		return err
	}
	*source = InstructionSource(decoded)
	return nil
}

type instructionEditDeclaration struct {
	Name        string
	Instruction Instruction
}

// InstructionEffectiveScope resolves the document-local scope identity used when merging
// instruction declarations.
func InstructionEffectiveScope(name string, rawScope string, header declaration.ManifestHeader) string {
	if strings.TrimSpace(rawScope) != "" {
		return strings.TrimSpace(rawScope)
	}
	if name == string(target.ScopeGlobal) || name == string(target.ScopeProject) {
		return name
	}
	return effectiveInstructionScope(rawScope, header)
}

// sameInstructionIdentity reports whether two instruction declarations may share one
// table while differing only in their explicit target sets.
func sameInstructionIdentity(name string, left Instruction, right Instruction, header declaration.ManifestHeader) bool {
	return left.Source == right.Source &&
		InstructionEffectiveScope(name, left.Scope, header) == InstructionEffectiveScope(name, right.Scope, header)
}

// ApplyInstructionAdd appends an instruction declaration or merges its explicit target
// set while preserving unrelated manifest bytes.
func ApplyInstructionAdd(original []byte, header declaration.ManifestHeader, name string, instruction Instruction) (declaration.EditResult, error) {
	value := instructionEditDeclaration{Name: name, Instruction: instruction}
	return declaration.ApplyAddDeclaration(declaration.AddEditInput[instructionEditDeclaration]{
		Original:    original,
		Header:      header,
		Declaration: value,
		Codec: declaration.AddEditContract[instructionEditDeclaration]{
			Kind: declaration.KindInstructions,
			Scan: scanInstructionEditBlocks,
			Key: func(value instructionEditDeclaration) (declaration.Key, error) {
				return declaration.Key{Kind: declaration.KindInstructions, Name: value.Name}, nil
			},
			ExplicitTargets: func(value instructionEditDeclaration) declaration.Targets {
				return declaration.Targets(value.Instruction.Targets)
			},
			SameIdentity: func(existing instructionEditDeclaration, incoming instructionEditDeclaration, header declaration.ManifestHeader) bool {
				return sameInstructionIdentity(incoming.Name, existing.Instruction, incoming.Instruction, header)
			},
			RenderBlock: func(value instructionEditDeclaration) string {
				return RenderInstructionBlock(value.Name, value.Instruction)
			},
			RenderBlockWithTargets: func(originalBlock string, existing instructionEditDeclaration, _ instructionEditDeclaration, mergedTargets declaration.Targets, _ declaration.ManifestHeader) (string, error) {
				return ReplaceInstructionTargets(originalBlock, existing.Name, mergedTargets.Values())
			},
			DuplicateError: func(key declaration.Key) error {
				return fmt.Errorf("duplicate instruction name %q", key.Name)
			},
			AlreadyExistsError: func(key declaration.Key) error {
				return fmt.Errorf("instruction %q already exists", key.Name)
			},
			InheritsTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("instruction %q inherits manifest targets; edit the manifest manually to change target inheritance", key.Name)
			},
			AlreadyHasTargetsError: func(key declaration.Key) error {
				return fmt.Errorf("instruction %q already has the selected targets", key.Name)
			},
		},
	})
}

func scanInstructionEditBlocks(content []byte) ([]declaration.EditBlock[instructionEditDeclaration], error) {
	blocks, err := ScanInstructionBlocks(content)
	if err != nil {
		return nil, err
	}
	declarations := make([]declaration.EditBlock[instructionEditDeclaration], 0, len(blocks))
	for _, block := range blocks {
		declarations = append(declarations, declaration.EditBlock[instructionEditDeclaration]{
			Range: declaration.DocumentRange{Start: block.Start, End: block.End},
			Value: instructionEditDeclaration{Name: block.Name, Instruction: block.Instruction},
		})
	}
	return declarations, nil
}

func effectiveInstructionScope(rawScope string, header declaration.ManifestHeader) string {
	return header.EffectiveScope(rawScope)
}

func ReplaceInstructionTargets(block string, instructionName string, targets []string) (string, error) {
	_ = instructionName
	updated, ok, err := declaration.ReplaceRootAssignment(block, "targets", renderStringArray(targets))
	if err != nil {
		return "", err
	}
	if ok {
		return updated, nil
	}
	return declaration.InsertRootAssignment(block, "targets", renderStringArray(targets))
}

func RemoveInstructionTargetTables(block string, instructionName string, selectedTargets []string) string {
	selected := make(map[string]struct{}, len(selectedTargets))
	for _, target := range selectedTargets {
		selected[target] = struct{}{}
	}

	lines := bytes.SplitAfter([]byte(block), []byte("\n"))
	ranges := make([]declaration.DocumentRange, 0)
	offset := 0
	activeStart := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if isSingleTOMLTableHeader(trimmed) {
			if activeStart >= 0 {
				ranges = append(ranges, declaration.DocumentRange{Start: activeStart, End: offset})
				activeStart = -1
			}
			name, target, ok := parseInstructionTargetHeader(trimmed)
			if ok && name == instructionName {
				if _, selectedTarget := selected[target]; selectedTarget {
					activeStart = offset
				}
			}
		}
		offset += len(line)
	}
	if activeStart >= 0 {
		ranges = append(ranges, declaration.DocumentRange{Start: activeStart, End: len(block)})
	}
	output := []byte(block)
	for index := len(ranges) - 1; index >= 0; index-- {
		output = declaration.RemoveDocumentRange(output, ranges[index])
	}
	return string(output)
}

func RenderInstructionBlock(name string, instruction Instruction) string {
	var builder strings.Builder
	builder.WriteString("[instructions.")
	builder.WriteString(strconv.Quote(name))
	builder.WriteString("]\n")
	builder.WriteString("source = ")
	builder.WriteString(renderInstructionSource(instruction.Source))
	builder.WriteByte('\n')
	if len(instruction.Targets) != 0 {
		builder.WriteString("targets = ")
		builder.WriteString(renderStringArray(instruction.Targets))
		builder.WriteByte('\n')
	}
	if instruction.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(instruction.Scope))
		builder.WriteByte('\n')
	}
	for _, targetName := range sortedInstructionRenderingTargets(instruction.Target) {
		rendering := instruction.Target[targetName]
		builder.WriteByte('\n')
		builder.WriteString("[instructions.")
		builder.WriteString(strconv.Quote(name))
		builder.WriteString(".target.")
		builder.WriteString(strconv.Quote(targetName))
		builder.WriteString("]\n")
		if rendering.RenderTo != "" {
			builder.WriteString("render_to = ")
			builder.WriteString(strconv.Quote(rendering.RenderTo))
			builder.WriteByte('\n')
		}
		if rendering.Mode != "" {
			builder.WriteString("mode = ")
			builder.WriteString(strconv.Quote(rendering.Mode))
			builder.WriteByte('\n')
		}
	}
	return builder.String()
}

func renderInstructionSource(source InstructionSource) string {
	if source.Git == "" && source.S3 == "" && source.Ref == "" && source.VersionID == "" &&
		source.Region == "" && source.Format == "" && source.Mode == "vendor" {
		return strconv.Quote(source.Path)
	}
	parts := make([]string, 0, 8)
	if source.Git != "" {
		parts = append(parts, "git = "+strconv.Quote(source.Git))
	}
	if source.Path != "" {
		parts = append(parts, "path = "+strconv.Quote(source.Path))
	}
	if source.Ref != "" {
		parts = append(parts, "ref = "+strconv.Quote(source.Ref))
	}
	if source.Mode != "" {
		parts = append(parts, "mode = "+strconv.Quote(source.Mode))
	}
	if source.S3 != "" {
		parts = append(parts, "s3 = "+strconv.Quote(source.S3))
	}
	if source.VersionID != "" {
		parts = append(parts, "version_id = "+strconv.Quote(source.VersionID))
	}
	if source.Region != "" {
		parts = append(parts, "region = "+strconv.Quote(source.Region))
	}
	if source.Format != "" {
		parts = append(parts, "format = "+strconv.Quote(source.Format))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func sortedInstructionRenderingTargets(renderings map[string]declaration.InstructionTarget) []string {
	targets := make([]string, 0, len(renderings))
	for targetName := range renderings {
		targets = append(targets, targetName)
	}
	sort.Strings(targets)
	return targets
}
