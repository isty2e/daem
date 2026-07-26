package merge

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
)

type existingDeclarations struct {
	Instructions []declarationcodec.InstructionBlock
	Skills       []declarationcodec.SkillBlock
	Hooks        []declarationcodec.HookBlock
	MCPServers   []declarationcodec.MCPServerBlock
}

func scanExistingDeclarations(content []byte) (existingDeclarations, error) {
	if err := validateManifestSyntax(content); err != nil {
		return existingDeclarations{}, fmt.Errorf("parse merge output manifest: %w", err)
	}
	instructions, err := declarationcodec.ScanInstructionBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	skills, err := declarationcodec.ScanSkillBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	hooks, err := declarationcodec.ScanHookBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	mcpServers, err := declarationcodec.ScanMCPServerBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	return existingDeclarations{
		Instructions: instructions,
		Skills:       skills,
		Hooks:        hooks,
		MCPServers:   mcpServers,
	}, nil
}

func validateManifestSyntax(content []byte) error {
	var decoded map[string]any
	_, err := toml.Decode(string(content), &decoded)
	return err
}

func sameInstructionSource(left declarationcodec.InstructionSource, right declarationcodec.InstructionSource) bool {
	return left.Git == right.Git &&
		left.Path == right.Path &&
		left.Ref == right.Ref &&
		effectiveSourceMode(left.Mode) == effectiveSourceMode(right.Mode) &&
		left.S3 == right.S3 &&
		left.VersionID == right.VersionID &&
		left.Region == right.Region &&
		left.Format == right.Format
}

func effectiveSourceMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return string(sourcepkg.LocalSourceModeVendor)
	}
	return mode
}

func effectiveInstallMode(mode string) string {
	if strings.TrimSpace(mode) == "" {
		return declarationInstallModeCopy
	}
	return mode
}
