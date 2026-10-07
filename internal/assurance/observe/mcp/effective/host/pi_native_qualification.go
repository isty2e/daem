package host

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

type piNativeSettings struct {
	mcpSelection    []string
	adapterDeclared bool
}

func qualifyPiNativeSettings(contract profile.PiMCPContract, scope target.Scope, workDir string, agentRoot string, version profile.PiMCPVersion) error {
	global, err := observePiNativeSettings(filepath.Join(agentRoot, "settings.json"))
	if err != nil {
		return err
	}
	project, err := observePiNativeSettings(filepath.Join(workDir, ".pi", "settings.json"))
	if err != nil {
		return err
	}
	return contract.QualifyNativeHost(profile.PiNativeHostFacts{
		Version: version, Scope: scope,
		BuiltinSelection: profile.ResolvePiMCPBuiltinSelection(global.mcpSelection, project.mcpSelection),
		AdapterDeclared:  global.adapterDeclared || project.adapterDeclared,
	})
}

func observePiNativeSettings(path string) (piNativeSettings, error) {
	content, exists, err := filesnapshot.ReadRegularFile(path, maximumConfigBytes)
	if err != nil {
		return piNativeSettings{}, fmt.Errorf("native Pi MCP settings cannot be read as a bounded regular file")
	}
	if !exists {
		return piNativeSettings{}, nil
	}
	if err := jsonstrict.Validate(content, "Pi native settings", maximumConfigDepth); err != nil {
		return piNativeSettings{}, fmt.Errorf("native Pi MCP settings require strict unambiguous JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil || fields == nil {
		return piNativeSettings{}, fmt.Errorf("native Pi MCP settings must be an object")
	}
	settings := piNativeSettings{}
	if raw, exists := fields["extensions"]; exists {
		if string(raw) == "null" || json.Unmarshal(raw, &settings.mcpSelection) != nil {
			return piNativeSettings{}, fmt.Errorf("native Pi MCP extension selection must be a string array")
		}
	}
	if raw, exists := fields["packages"]; exists {
		var packages []json.RawMessage
		if string(raw) == "null" || json.Unmarshal(raw, &packages) != nil {
			return piNativeSettings{}, fmt.Errorf("native Pi MCP package selection must be an array")
		}
		for _, item := range packages {
			var source string
			if json.Unmarshal(item, &source) != nil {
				var entry struct {
					Source string `json:"source"`
				}
				if json.Unmarshal(item, &entry) != nil || entry.Source == "" {
					return piNativeSettings{}, fmt.Errorf("native Pi MCP package source cannot be observed")
				}
				source = entry.Source
			}
			if profile.PiMCPAdapterPackageSource(source) {
				settings.adapterDeclared = true
			}
		}
	}
	return settings, nil
}
