package codec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

type Extension struct {
	ID      string          `toml:"id"`
	Carrier string          `toml:"carrier"`
	Targets []string        `toml:"targets"`
	Scope   string          `toml:"scope"`
	Source  ExtensionSource `toml:"source"`
}

type ExtensionSource struct {
	Marketplace string `toml:"marketplace"`
	HostSource  string `toml:"host_source"`
}

// Ref returns the populated external source reference without interpreting its
// host-specific kind.
func (source ExtensionSource) Ref() string {
	if source.HostSource != "" {
		return source.HostSource
	}
	return source.Marketplace
}

// SameExtensionRelation reports whether two extension declarations describe the same
// document-local relation independently of their declaration IDs.
func SameExtensionRelation(left Extension, right Extension) bool {
	return left.Carrier == right.Carrier &&
		left.Scope == right.Scope &&
		equalExtensionStringValues(left.Targets, right.Targets) &&
		left.Source == right.Source
}

func equalExtensionStringValues(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

type ExtensionBlock struct {
	Start     int
	End       int
	Extension Extension
}

func ScanExtensionBlocks(content []byte) ([]ExtensionBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return declaration.StartsArrayTableRoot(trimmed, "extension") },
		startsNewExtensionTable,
	)
	blocks := make([]ExtensionBlock, 0, len(ranges))
	for _, targetRange := range ranges {
		block, err := parseExtensionBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func startsNewExtensionTable(trimmedLine string) bool {
	return declaration.StartsTableOutsideRoot(trimmedLine, "extension")
}

func parseExtensionBlock(content []byte, start int, end int) (ExtensionBlock, error) {
	var decoded struct {
		Extensions []Extension `toml:"extension"`
	}
	if _, err := toml.Decode(string(content[start:end]), &decoded); err != nil {
		return ExtensionBlock{}, fmt.Errorf("parse existing extension block: %w", err)
	}
	if len(decoded.Extensions) != 1 {
		return ExtensionBlock{}, fmt.Errorf("parse existing extension block: expected one extension")
	}
	return ExtensionBlock{
		Start:     start,
		End:       end,
		Extension: decoded.Extensions[0],
	}, nil
}

func RenderExtensionBlock(extension Extension) string {
	var builder strings.Builder
	builder.WriteString("[[extension]]\n")
	builder.WriteString("id = ")
	builder.WriteString(strconv.Quote(extension.ID))
	builder.WriteByte('\n')
	builder.WriteString("carrier = ")
	builder.WriteString(strconv.Quote(extension.Carrier))
	builder.WriteByte('\n')
	if len(extension.Targets) != 0 {
		builder.WriteString("targets = ")
		builder.WriteString(renderExtensionStringArray(extension.Targets))
		builder.WriteByte('\n')
	}
	if extension.Scope != "" {
		builder.WriteString("scope = ")
		builder.WriteString(strconv.Quote(extension.Scope))
		builder.WriteByte('\n')
	}
	builder.WriteString("source = ")
	builder.WriteString(renderExtensionSource(extension.Source))
	builder.WriteByte('\n')
	return builder.String()
}

func renderExtensionSource(source ExtensionSource) string {
	parts := make([]string, 0, 2)
	if source.Marketplace != "" {
		parts = append(parts, "marketplace = "+strconv.Quote(source.Marketplace))
	}
	if source.HostSource != "" {
		parts = append(parts, "host_source = "+strconv.Quote(source.HostSource))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func renderExtensionStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
