package host

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

type piNativeSettings struct {
	mcpSelection    []string
	adapterPackages []piNativeAdapterPackage
	npmCommand      json.RawMessage
}

func qualifyPiNativeSettings(ctx context.Context, contract profile.PiMCPContract, scope target.Scope, workDir string, agentRoot string, version profile.PiMCPVersion) error {
	global, err := observePiNativeSettings(ctx, filepath.Join(agentRoot, "settings.json"), target.ScopeGlobal)
	if err != nil {
		return err
	}
	project, err := observePiNativeSettings(ctx, filepath.Join(workDir, ".pi", "settings.json"), target.ScopeProject)
	if err != nil {
		return err
	}

	type packageSelection struct {
		packages []piNativeAdapterPackage
		settings piNativePackageContext
	}
	user := piNativePackageContext{workDir: workDir, agentRoot: agentRoot, npmCommand: global.npmCommand}
	participating := user
	participating.projectNpmCommand = project.npmCommand
	contexts := []packageSelection{
		{selectNativeAdapterPackages(global.adapterPackages, project.adapterPackages), participating},
		{selectNativeAdapterPackages(global.adapterPackages, nil), user},
	}
	adapterSelected := false
	for _, selection := range contexts {
		selected, err := nativeAdapterResourcesSelected(ctx, selection.packages, selection.settings)
		if err != nil {
			return err
		}
		adapterSelected = adapterSelected || selected
	}
	return contract.QualifyNativeHost(profile.PiNativeHostFacts{
		Version: version, Scope: scope,
		BuiltinSelection: profile.ResolvePiMCPBuiltinSelection(global.mcpSelection, project.mcpSelection),
		AdapterDeclared:  adapterSelected,
	})
}

func observePiNativeSettings(ctx context.Context, path string, scope target.Scope) (piNativeSettings, error) {
	content, exists, err := filesnapshot.ReadRegularFileContext(ctx, path, maximumConfigBytes)
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
	settings.npmCommand = fields["npmCommand"]
	if raw, exists := fields["extensions"]; exists {
		settings.mcpSelection, err = nativeSettingsStringArray(raw)
		if err != nil {
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
			var entry map[string]json.RawMessage
			if json.Unmarshal(item, &source) != nil {
				if json.Unmarshal(item, &entry) != nil || entry == nil || json.Unmarshal(entry["source"], &source) != nil || source == "" {
					return piNativeSettings{}, fmt.Errorf("native Pi MCP package source cannot be observed")
				}
			}
			if source == "" {
				return piNativeSettings{}, fmt.Errorf("native Pi MCP package source cannot be observed")
			}
			knownAdapter, err := profile.PiMCPAdapterPackageSource(source)
			if err != nil {
				return piNativeSettings{}, err
			}
			if knownAdapter {
				if !strings.HasPrefix(source, "npm:") {
					return piNativeSettings{}, fmt.Errorf("native Pi MCP cannot qualify a Git Adapter package through the npm selector contract")
				}
				adapter := piNativeAdapterPackage{source: source, scope: scope}
				if raw, exists := entry["extensions"]; exists {
					adapter.patterns, err = nativeSettingsStringArray(raw)
					if err != nil {
						return piNativeSettings{}, fmt.Errorf("native Pi MCP package extension selection must be a string array")
					}
					adapter.filtered = true
				}
				if raw, exists := entry["autoload"]; exists {
					var autoload bool
					if string(raw) == "null" || json.Unmarshal(raw, &autoload) != nil {
						return piNativeSettings{}, fmt.Errorf("native Pi MCP package autoload must be boolean")
					}
					adapter.delta = !autoload
				}
				settings.adapterPackages = append(settings.adapterPackages, adapter)
			} else {
				name, err := observeNativeLocalPackageName(ctx, source, filepath.Dir(path))
				if err != nil {
					return piNativeSettings{}, err
				}
				if name == "pi-mcp-adapter" {
					return piNativeSettings{}, fmt.Errorf("native Pi MCP cannot qualify a local Adapter package through the npm selector contract")
				}
			}
		}
	}
	return settings, nil
}

func nativeSettingsStringArray(raw json.RawMessage) ([]string, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return nil, fmt.Errorf("expected a string array")
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		var value string
		if string(item) == "null" || json.Unmarshal(item, &value) != nil {
			return nil, fmt.Errorf("expected a string array")
		}
		result = append(result, value)
	}
	return result, nil
}
