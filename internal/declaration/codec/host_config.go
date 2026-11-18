package codec

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
	fields, err := hostConfigFields(config)
	if err != nil {
		return "", err
	}
	return "[[host_config]]\n" + strings.Join(fields, "\n") + "\n", nil
}

func hostConfigFields(config hostconfig.Config) ([]string, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("render host_config: %w", err)
	}

	sourceFields := hostConfigDeclarationSource(config.Source())
	for _, value := range []string{sourceFields.Path, sourceFields.Git, sourceFields.Ref, sourceFields.S3, sourceFields.VersionID, sourceFields.Region} {
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("host configuration source must be representable as UTF-8 TOML")
		}
	}
	quote := tomlsyntax.QuoteBasicString
	fields := []string{
		"id = " + quote(config.ID().Name()),
		"targets = [" + quote(string(config.Target())) + "]",
		"scope = " + quote(string(config.Scope())),
		"path = " + quote(config.Path()),
		"source = " + renderInstructionSourceWithQuote(InstructionSource(sourceFields), quote),
	}
	if config.Ownership() != hostconfig.OwnershipKeys {
		fields = append(fields, "ownership = "+quote(string(config.Ownership())))
	}
	if config.Syntax() != hostconfig.SyntaxDefault {
		fields = append(fields, "syntax = "+quote(string(config.Syntax())))
	}
	return fields, nil
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
