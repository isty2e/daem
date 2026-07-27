package codec

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/declaration"
)

// ImportManifestSource is the local source form emitted by adoption.
type ImportManifestSource struct {
	Path string `toml:"path"`
	Mode string `toml:"mode"`
}

// ImportManifestSkill is the omission-sensitive skill row emitted by adoption.
type ImportManifestSkill struct {
	ID          string                             `toml:"id,omitempty"`
	Name        string                             `toml:"name"`
	Source      ImportManifestSource               `toml:"source"`
	Targets     []string                           `toml:"targets"`
	Scope       string                             `toml:"scope"`
	InstallMode string                             `toml:"install_mode"`
	Target      map[string]declaration.SkillTarget `toml:"target,omitempty"`
}

// ImportManifestSkillGroup is the omission-sensitive skill-group row emitted by adoption.
type ImportManifestSkillGroup struct {
	Names       []string                           `toml:"names"`
	Source      ImportManifestSource               `toml:"source"`
	Targets     []string                           `toml:"targets"`
	Scope       string                             `toml:"scope"`
	InstallMode string                             `toml:"install_mode"`
	Target      map[string]declaration.SkillTarget `toml:"target,omitempty"`
}

// ImportManifestInstruction is the path-source instruction row emitted by adoption.
type ImportManifestInstruction struct {
	Source  string                                        `toml:"source"`
	Targets []string                                      `toml:"targets"`
	Scope   string                                        `toml:"scope"`
	Target  map[string]ImportManifestInstructionRendering `toml:"target,omitempty"`
}

// ImportManifestInstructionRendering is the omission-sensitive target rendering emitted by adoption.
type ImportManifestInstructionRendering struct {
	RenderTo string `toml:"render_to,omitempty"`
	Mode     string `toml:"mode,omitempty"`
}

// ImportManifestHook is the command-hook row emitted by adoption.
type ImportManifestHook struct {
	Name            string                             `toml:"name"`
	Event           string                             `toml:"event"`
	Matcher         string                             `toml:"matcher"`
	Type            string                             `toml:"type"`
	Command         string                             `toml:"command"`
	Timeout         int                                `toml:"timeout"`
	StatusMessage   string                             `toml:"status_message"`
	Targets         []string                           `toml:"targets"`
	Scope           string                             `toml:"scope"`
	TargetOverrides []ImportManifestHookTargetOverride `toml:"target_override"`
}

// ImportManifestHookTargetOverride is the reduced hook override emitted by adoption.
type ImportManifestHookTargetOverride struct {
	Target    string `toml:"target"`
	Condition string `toml:"if"`
}

// ImportManifestBody is the declaration-owned import-render view.
type ImportManifestBody struct {
	Instructions map[string]ImportManifestInstruction `toml:"instructions"`
	SkillGroups  []ImportManifestSkillGroup           `toml:"skill_group"`
	Skills       []ImportManifestSkill                `toml:"skill"`
	Hooks        []ImportManifestHook                 `toml:"hook"`
	MCPServers   []MCPServer                          `toml:"mcp_server"`
}

type importManifest struct {
	Version int      `toml:"version"`
	Targets []string `toml:"targets"`
	ImportManifestBody
}

// RenderImportManifest renders one complete manifest from the import operation view.
func RenderImportManifest(targets []string, body ImportManifestBody) ([]byte, error) {
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(importManifest{
		Version:            1,
		Targets:            targets,
		ImportManifestBody: body,
	}); err != nil {
		return nil, fmt.Errorf("render import manifest: %w", err)
	}

	return output.Bytes(), nil
}

// RenderImportManifestBody renders only resource declarations for an existing manifest.
func RenderImportManifestBody(body ImportManifestBody) ([]byte, error) {
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(body); err != nil {
		return nil, fmt.Errorf("render import manifest body: %w", err)
	}

	return compactImportManifestBody(output.Bytes()), nil
}

func compactImportManifestBody(content []byte) []byte {
	lines := bytes.SplitAfter(content, []byte("\n"))
	var output bytes.Buffer
	for _, line := range lines {
		switch strings.TrimSpace(string(line)) {
		case "", "instructions = {}", "skill_group = []", "skill = []", "hook = []", "mcp_server = []":
			continue
		default:
			output.Write(line)
		}
	}
	return output.Bytes()
}

// AppendImportManifestBody appends rendered import declarations without rewriting existing bytes.
func AppendImportManifestBody(content []byte, body []byte) []byte {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return append([]byte{}, content...)
	}
	output := append([]byte{}, content...)
	if len(output) != 0 && !bytes.HasSuffix(output, []byte("\n")) {
		output = append(output, '\n')
	}
	if len(output) != 0 {
		output = append(output, '\n')
	}
	output = append(output, body...)
	output = append(output, '\n')
	return output
}
