package skillcompat

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"gopkg.in/yaml.v3"
)

var utf8BOM = []byte{0xef, 0xbb, 0xbf}

// LoadSkillFrontmatter reads and parses the YAML frontmatter from a skill artifact.
func LoadSkillFrontmatter(
	ctx context.Context,
	view access.View,
	sourceID artifact.SourceID,
) (SkillFrontmatter, error) {
	if _, err := exactSkillFile(ctx, view, sourceID); err != nil {
		return SkillFrontmatter{}, err
	}
	content, err := ReadSkillDocument(ctx, view, "SKILL.md")
	if err != nil {
		return SkillFrontmatter{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	return ParseSkillFrontmatter(content.Bytes())
}

// SkillFrontmatter is the canonical top-level metadata extracted from SKILL.md.
type SkillFrontmatter struct {
	Name         string
	Description  string
	Fields       map[string]struct{}
	stringFields map[string]string
}

// StringField returns a normalized string or null field value when present.
func (frontmatter SkillFrontmatter) StringField(field string) (string, bool) {
	if _, present := frontmatter.Fields[field]; !present {
		return "", false
	}
	value, present := frontmatter.stringFields[field]
	return value, present
}

// ParseSkillFrontmatter parses canonical frontmatter from exact SKILL.md bytes.
func ParseSkillFrontmatter(content []byte) (SkillFrontmatter, error) {
	if err := CheckSkillDocumentSize(int64(len(content))); err != nil {
		return SkillFrontmatter{}, err
	}
	normalized := normalizeFrontmatterBytes(content)
	firstLine, remainder, hasNewline := bytes.Cut(normalized, []byte("\n"))
	if !hasNewline {
		remainder = nil
	}

	if !isFrontmatterDelimiter(firstLine) {
		return SkillFrontmatter{}, fmt.Errorf("SKILL.md frontmatter is required")
	}

	rawFrontmatter, ok := frontmatterBody(remainder)
	if !ok {
		return SkillFrontmatter{}, fmt.Errorf("SKILL.md frontmatter closing delimiter is required")
	}

	frontmatter := SkillFrontmatter{
		Fields:       make(map[string]struct{}),
		stringFields: make(map[string]string),
	}
	if len(bytes.TrimSpace(rawFrontmatter)) == 0 {
		return frontmatter, nil
	}

	var document yaml.Node
	if err := yaml.Unmarshal(rawFrontmatter, &document); err != nil {
		return SkillFrontmatter{}, fmt.Errorf("parse SKILL.md frontmatter YAML: %w", err)
	}

	root := yamlDocumentRoot(document)
	if root == nil {
		return frontmatter, nil
	}
	if root.Kind != yaml.MappingNode {
		return SkillFrontmatter{}, fmt.Errorf("SKILL.md frontmatter must be a YAML mapping")
	}

	for index := 0; index < len(root.Content); index += 2 {
		keyNode := root.Content[index]
		valueNode := root.Content[index+1]
		if keyNode.Kind != yaml.ScalarNode || keyNode.Value == "" {
			return SkillFrontmatter{}, fmt.Errorf("SKILL.md frontmatter field names must be non-empty scalars")
		}
		field := keyNode.Value
		if _, exists := frontmatter.Fields[field]; exists {
			return SkillFrontmatter{}, fmt.Errorf("SKILL.md frontmatter field %q is duplicated", field)
		}
		frontmatter.Fields[field] = struct{}{}
		if value, ok := optionalFrontmatterStringValue(valueNode, 0); ok {
			frontmatter.stringFields[field] = value
		}
		switch field {
		case "name":
			value, err := frontmatterStringValue(valueNode, field)
			if err != nil {
				return SkillFrontmatter{}, err
			}
			frontmatter.Name = value
			frontmatter.stringFields[field] = value
		case "description":
			value, err := frontmatterStringValue(valueNode, field)
			if err != nil {
				return SkillFrontmatter{}, err
			}
			frontmatter.Description = value
			frontmatter.stringFields[field] = value
		}
	}

	return frontmatter, nil
}

func optionalFrontmatterStringValue(node *yaml.Node, depth int) (string, bool) {
	if depth > 8 {
		return "", false
	}
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		return optionalFrontmatterStringValue(node.Alias, depth+1)
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return "", true
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", false
	}
	return strings.TrimSpace(node.Value), true
}

func normalizeFrontmatterBytes(content []byte) []byte {
	normalized := bytes.TrimPrefix(content, utf8BOM)
	normalized = bytes.ReplaceAll(normalized, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(normalized, []byte("\r"), []byte("\n"))
}

func frontmatterBody(content []byte) ([]byte, bool) {
	lines := bytes.Split(content, []byte("\n"))
	bodyLines := make([][]byte, 0, len(lines))
	for _, line := range lines {
		if isFrontmatterDelimiter(line) {
			return bytes.Join(bodyLines, []byte("\n")), true
		}
		bodyLines = append(bodyLines, line)
	}

	return nil, false
}

func isFrontmatterDelimiter(line []byte) bool {
	return bytes.Equal(bytes.TrimRight(line, " \t"), []byte("---"))
}

func yamlDocumentRoot(document yaml.Node) *yaml.Node {
	if document.Kind != yaml.DocumentNode || len(document.Content) == 0 {
		return nil
	}

	root := document.Content[0]
	if root.Kind == yaml.ScalarNode && root.Tag == "!!null" {
		return nil
	}

	return root
}

func frontmatterStringValue(node *yaml.Node, field string) (string, error) {
	return frontmatterStringValueAtDepth(node, field, 0)
}

func frontmatterStringValueAtDepth(node *yaml.Node, field string, depth int) (string, error) {
	if depth > 8 {
		return "", fmt.Errorf("SKILL.md frontmatter field %q alias chain is too deep", field)
	}
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		return frontmatterStringValueAtDepth(node.Alias, field, depth+1)
	}
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return "", nil
	}
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("SKILL.md frontmatter field %q must be a string", field)
	}

	return strings.TrimSpace(node.Value), nil
}
