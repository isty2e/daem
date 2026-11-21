package codec

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/declaration"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/supply/source"
)

var ErrAgentConfigBlockSelection = errors.New("expected one matching [[agent_config]] block")

func selectAgentConfigBlock(content []byte, id string) (declaration.DocumentRange, error) {
	ranges := declaration.ScanDocumentRanges(
		content,
		func(trimmed string) bool { return tomlsyntax.StartsArrayTableRoot(trimmed, "agent_config") },
		func(trimmed string) bool { return tomlsyntax.StartsTableOutsideRoot(trimmed, "agent_config") },
	)
	var selected declaration.DocumentRange
	matches := 0
	for _, targetRange := range ranges {
		var decoded struct {
			AgentConfigs []declaration.AgentConfig `toml:"agent_config"`
		}
		if err := decodeBlockFields(content, targetRange, &decoded); err != nil {
			return declaration.DocumentRange{}, fmt.Errorf("parse existing agent_config block: %w", err)
		}
		if len(decoded.AgentConfigs) != 1 {
			return declaration.DocumentRange{}, fmt.Errorf("parse existing agent_config block: expected one agent_config")
		}
		if decoded.AgentConfigs[0].ID == id {
			selected = targetRange
			matches++
		}
	}
	if matches != 1 || id == "" {
		return declaration.DocumentRange{}, ErrAgentConfigBlockSelection
	}
	return selected, nil
}

// RenderAgentConfigBlock encodes one validated agent configuration declaration.
func RenderAgentConfigBlock(config agentconfig.Config) (string, error) {
	fields, err := agentConfigFields(config)
	if err != nil {
		return "", err
	}
	return "[[agent_config]]\n" + strings.Join(fields, "\n") + "\n", nil
}

func agentConfigFields(config agentconfig.Config) ([]string, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("render agent_config: %w", err)
	}

	sourceFields := agentConfigDeclarationSource(config.Source())
	for _, value := range []string{sourceFields.Path, sourceFields.Git, sourceFields.Ref, sourceFields.S3, sourceFields.VersionID, sourceFields.Region} {
		if !utf8.ValidString(value) {
			return nil, fmt.Errorf("agent configuration source must be representable as UTF-8 TOML")
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
