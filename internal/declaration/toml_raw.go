package declaration

import (
	"fmt"
	"strings"
)

type Manifest struct {
	Version      int                     `toml:"version"`
	Targets      []string                `toml:"targets"`
	Defaults     Defaults                `toml:"defaults"`
	Skills       []Skill                 `toml:"skill"`
	SkillGroups  []SkillGroup            `toml:"skill_group"`
	Hooks        []Hook                  `toml:"hook"`
	HookAssets   map[string]HookAsset    `toml:"hook_asset"`
	Instructions map[string]Instructions `toml:"instructions"`
	MCPServers   []MCPServer             `toml:"mcp_server"`
	Extensions   []Extension             `toml:"extension"`
}

type Defaults struct {
	Scope       string `toml:"scope"`
	InstallMode string `toml:"install_mode"`
}

type Source struct {
	Git       string `toml:"git"`
	Path      string `toml:"path"`
	Ref       string `toml:"ref"`
	Mode      string `toml:"mode"`
	S3        string `toml:"s3"`
	VersionID string `toml:"version_id"`
	Region    string `toml:"region"`
	Format    string `toml:"format"`
}

type Skill struct {
	ID           string `toml:"id"`
	Name         string `toml:"name"`
	Source       Source `toml:"source"`
	Targets      []string
	Scope        string `toml:"scope"`
	InstallMode  string `toml:"install_mode"`
	Portable     *bool  `toml:"portable"`
	CompatRepair bool   `toml:"compat_repair"`
}

type SkillGroup struct {
	Names        []string `toml:"names"`
	Include      []string `toml:"include"`
	Exclude      []string `toml:"exclude"`
	Source       Source   `toml:"source"`
	Targets      []string `toml:"targets"`
	Scope        string   `toml:"scope"`
	InstallMode  string   `toml:"install_mode"`
	Portable     *bool    `toml:"portable"`
	CompatRepair bool     `toml:"compat_repair"`
}

type Hook struct {
	Name            string               `toml:"name"`
	Event           string               `toml:"event"`
	Matcher         string               `toml:"matcher"`
	Type            string               `toml:"type"`
	Command         string               `toml:"command"`
	TimeoutSeconds  int                  `toml:"timeout"`
	StatusMessage   string               `toml:"status_message"`
	Targets         []string             `toml:"targets"`
	Scope           string               `toml:"scope"`
	TargetOverrides []HookTargetOverride `toml:"target_override"`
}

type HookTargetOverride struct {
	Target    string `toml:"target"`
	Condition string `toml:"if"`
	Matcher   string `toml:"matcher"`
}

type HookAsset struct {
	Source     HookAssetSource `toml:"source"`
	Kind       string          `toml:"kind"`
	Scope      string          `toml:"scope"`
	Executable bool            `toml:"executable"`
}

type HookAssetSource struct {
	Source Source
	Set    bool
}

func (source *HookAssetSource) UnmarshalTOML(value any) error {
	source.Set = true

	raw, err := SourceFromTOMLValue(value)
	if err != nil {
		return err
	}
	source.Source = raw
	return nil
}

type MCPServer struct {
	Name      string                     `toml:"name"`
	Targets   []string                   `toml:"targets"`
	Scope     string                     `toml:"scope"`
	Transport string                     `toml:"transport"`
	Command   string                     `toml:"command"`
	Args      []string                   `toml:"args"`
	Env       map[string]MCPEnvReference `toml:"env"`
}

type MCPEnvReference struct {
	FromEnv string
}

func (reference *MCPEnvReference) UnmarshalTOML(value any) error {
	values, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("env reference must be an inline table with from_env")
	}

	for key, raw := range values {
		if key != "from_env" {
			return fmt.Errorf("unknown env reference key %q", key)
		}
		text, ok := raw.(string)
		if !ok {
			return fmt.Errorf("env reference from_env must be a string")
		}
		reference.FromEnv = text
	}

	return nil
}

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

// Ref returns the populated external reference without interpreting its
// host-specific source kind.
func (source ExtensionSource) Ref() string {
	if source.HostSource != "" {
		return source.HostSource
	}
	return source.Marketplace
}

type Instructions struct {
	Source  InstructionSource            `toml:"source"`
	Targets []string                     `toml:"targets"`
	Scope   string                       `toml:"scope"`
	Target  map[string]InstructionTarget `toml:"target"`
}

type InstructionSource struct {
	Source Source
	Set    bool
}

func (source *InstructionSource) UnmarshalTOML(value any) error {
	source.Set = true

	raw, err := SourceFromTOMLValue(value)
	if err != nil {
		return err
	}
	source.Source = raw
	return nil
}

// SourceFromTOMLValue decodes the manifest source grammar shared by source
// fields that accept either a path string or an inline table.
func SourceFromTOMLValue(value any) (Source, error) {
	switch typed := value.(type) {
	case string:
		return Source{
			Path: typed,
			Mode: "vendor",
		}, nil
	case map[string]any:
		return sourceFromInlineTable(typed)
	default:
		return Source{}, fmt.Errorf("source must be a string path or inline table")
	}
}

func sourceFromInlineTable(values map[string]any) (Source, error) {
	var source Source
	for key, value := range values {
		text, ok := value.(string)
		if !ok {
			return Source{}, fmt.Errorf("source.%s: must be a string", key)
		}

		switch strings.TrimSpace(key) {
		case "git":
			source.Git = text
		case "path":
			source.Path = text
		case "ref":
			source.Ref = text
		case "mode":
			source.Mode = text
		case "s3":
			source.S3 = text
		case "version_id":
			source.VersionID = text
		case "region":
			source.Region = text
		case "format":
			source.Format = text
		default:
			return Source{}, fmt.Errorf("unknown source key %q", key)
		}
	}

	return source, nil
}

type InstructionTarget struct {
	RenderTo string `toml:"render_to"`
	Mode     string `toml:"mode"`
}
