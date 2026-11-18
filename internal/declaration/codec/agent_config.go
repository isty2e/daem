package codec

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/supply/source"
)

// AgentConfigBlock is one host_config array-table range and its decoded value.
type AgentConfigBlock struct {
	Start       int
	End         int
	AgentConfig declaration.AgentConfig
}

// ScanAgentConfigBlocks finds top-level host_config rows and their byte ranges.
func ScanAgentConfigBlocks(content []byte) ([]AgentConfigBlock, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return tomlsyntax.StartsArrayTableRoot(trimmed, "host_config") },
		startsNewAgentConfigTable,
	)
	blocks := make([]AgentConfigBlock, 0, len(ranges))
	for _, targetRange := range ranges {
		block, err := parseAgentConfigBlock(content, targetRange.Start, targetRange.End)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func startsNewAgentConfigTable(trimmedLine string) bool {
	return tomlsyntax.StartsTableOutsideRoot(trimmedLine, "host_config")
}

func parseAgentConfigBlock(content []byte, start int, end int) (AgentConfigBlock, error) {
	var decoded struct {
		AgentConfigs []declaration.AgentConfig `toml:"host_config"`
	}
	if err := decodeBlockFields(content, declaration.DocumentRange{Start: start, End: end}, &decoded); err != nil {
		return AgentConfigBlock{}, fmt.Errorf("parse existing host_config block: %w", err)
	}
	if len(decoded.AgentConfigs) != 1 {
		return AgentConfigBlock{}, fmt.Errorf("parse existing host_config block: expected one host_config")
	}
	return AgentConfigBlock{
		Start: start, End: end, AgentConfig: decoded.AgentConfigs[0],
	}, nil
}

// RenderAgentConfigBlock encodes one validated canonical host configuration.
func RenderAgentConfigBlock(config agentconfig.Config) (string, error) {
	fields, err := agentConfigFields(config)
	if err != nil {
		return "", err
	}
	return "[[host_config]]\n" + strings.Join(fields, "\n") + "\n", nil
}

func agentConfigFields(config agentconfig.Config) ([]string, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("render host_config: %w", err)
	}

	sourceFields := agentConfigDeclarationSource(config.Source())
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
	if config.Ownership() != agentconfig.OwnershipKeys {
		fields = append(fields, "ownership = "+quote(string(config.Ownership())))
	}
	if config.Syntax() != agentconfig.SyntaxDefault {
		fields = append(fields, "syntax = "+quote(string(config.Syntax())))
	}
	return fields, nil
}

func agentConfigDeclarationSource(value source.Source) declaration.Source {
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
