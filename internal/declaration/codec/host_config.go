package codec

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/supply/source"
)

// HostConfigBlock is one host_config array-table range and its decoded value.
type HostConfigBlock struct {
	Start      int
	End        int
	HostConfig declaration.HostConfig
}

// ScanHostConfigBlocks finds top-level host_config rows and their byte ranges.
func ScanHostConfigBlocks(content []byte) ([]HostConfigBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return tomlsyntax.StartsArrayTableRoot(trimmed, "host_config") },
		startsNewHostConfigTable,
	)
	blocks := make([]HostConfigBlock, 0, len(ranges))
	for _, targetRange := range ranges {
		block, err := parseHostConfigBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func startsNewHostConfigTable(trimmedLine string) bool {
	return tomlsyntax.StartsTableOutsideRoot(trimmedLine, "host_config")
}

func parseHostConfigBlock(content []byte, start int, end int) (HostConfigBlock, error) {
	var decoded struct {
		HostConfigs []declaration.HostConfig `toml:"host_config"`
	}
	if err := decodeBlockFields(content, declaration.DocumentRange{Start: start, End: end}, &decoded); err != nil {
		return HostConfigBlock{}, fmt.Errorf("parse existing host_config block: %w", err)
	}
	if len(decoded.HostConfigs) != 1 {
		return HostConfigBlock{}, fmt.Errorf("parse existing host_config block: expected one host_config")
	}
	return HostConfigBlock{
		Start: start, End: end, HostConfig: decoded.HostConfigs[0],
	}, nil
}

// RenderHostConfigBlock encodes one validated canonical host configuration.
func RenderHostConfigBlock(config hostconfig.Config) (string, error) {
	if err := config.Validate(); err != nil {
		return "", fmt.Errorf("render host_config: %w", err)
	}

	var builder strings.Builder
	builder.WriteString("[[host_config]]\n")
	builder.WriteString("id = ")
	builder.WriteString(strconv.Quote(config.ID().Name()))
	builder.WriteByte('\n')
	builder.WriteString("targets = ")
	builder.WriteString(renderStringArray([]string{string(config.Target())}))
	builder.WriteByte('\n')
	builder.WriteString("scope = ")
	builder.WriteString(strconv.Quote(string(config.Scope())))
	builder.WriteByte('\n')
	builder.WriteString("path = ")
	builder.WriteString(strconv.Quote(config.Path()))
	builder.WriteByte('\n')
	builder.WriteString("source = ")
	builder.WriteString(renderInstructionSource(InstructionSource(hostConfigDeclarationSource(config.Source()))))
	builder.WriteByte('\n')
	if config.Ownership() != hostconfig.OwnershipKeys {
		builder.WriteString("ownership = ")
		builder.WriteString(strconv.Quote(string(config.Ownership())))
		builder.WriteByte('\n')
	}
	if config.Syntax() != hostconfig.SyntaxDefault {
		builder.WriteString("syntax = ")
		builder.WriteString(strconv.Quote(string(config.Syntax())))
		builder.WriteByte('\n')
	}
	return builder.String(), nil
}

func hostConfigDeclarationSource(value source.Source) declaration.Source {
	switch value.Kind() {
	case source.SourceKindLocal:
		local, _ := value.Local()
		return declaration.Source{Path: local.Path(), Mode: string(local.Mode())}
	case source.SourceKindGit:
		git, _ := value.Git()
		return declaration.Source{
			Git:  git.Locator().String(),
			Path: git.RepositoryPath().String(),
			Ref:  git.Ref().String(),
		}
	case source.SourceKindS3:
		object, _ := value.S3()
		return declaration.Source{
			S3:        object.ObjectURI().Canonical(),
			VersionID: object.VersionID(),
			Region:    object.Region(),
			Format:    string(object.Format()),
		}
	default:
		return declaration.Source{}
	}
}
