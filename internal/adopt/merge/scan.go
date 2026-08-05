package merge

import (
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
)

type existingDeclarations struct {
	Header       declaration.ManifestHeader
	Instructions []declarationcodec.InstructionBlock
	Skills       []declarationcodec.SkillBlock
	Hooks        []declarationcodec.HookBlock
	MCPServers   []declarationcodec.MCPServerBlock
	Extensions   []declarationcodec.ExtensionBlock
}

func scanExistingDeclarations(content []byte) (existingDeclarations, error) {
	if err := validateCanonicalManifest(content); err != nil {
		return existingDeclarations{}, fmt.Errorf("decode merge output manifest: %w", err)
	}
	header, err := declaration.DecodeManifestHeader(content)
	if err != nil {
		return existingDeclarations{}, err
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
	extensions, err := declarationcodec.ScanExtensionBlocks(content)
	if err != nil {
		return existingDeclarations{}, err
	}
	return existingDeclarations{
		Header:       header,
		Instructions: instructions,
		Skills:       skills,
		Hooks:        hooks,
		MCPServers:   mcpServers,
		Extensions:   extensions,
	}, nil
}

func validateCanonicalManifest(content []byte) error {
	_, err := declarationmanifest.Decode(content)
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
