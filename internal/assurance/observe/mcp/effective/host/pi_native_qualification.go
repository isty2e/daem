package host

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

type piNativeSettings struct {
	extensionEntries []string
	adapterDeclared  bool
}

func qualifyPiNativeSettings(ctx context.Context, contract profile.PiMCPContract, scope target.Scope, workDir string, agentRoot string, version profile.PiMCPVersion) error {
	if err := contract.QualifyNativeVersion(version); err != nil {
		return err
	}
	global, err := observePiNativeSettings(ctx, filepath.Join(agentRoot, "settings.json"))
	if err != nil {
		return err
	}
	project, err := observePiNativeSettings(ctx, filepath.Join(workDir, ".pi", "settings.json"))
	if err != nil {
		return err
	}

	return contract.QualifyNativeHost(profile.PiNativeHostFacts{
		Version: version, Scope: scope,
		BuiltinSelection: profile.ResolvePiMCPBuiltinSelection(global.extensionEntries, project.extensionEntries),
		AdapterDeclared:  global.adapterDeclared || project.adapterDeclared,
	})
}

func nativeHostPrerequisite(err error) (mcpobserve.HostPrerequisiteObservation, error) {
	input := mcpobserve.HostPrerequisiteObservationInput{State: mcpobserve.HostQualified}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return mcpobserve.HostPrerequisiteObservation{}, err
		}
		input.State = mcpobserve.HostUnqualified
		input.Detail = "Native Pi settings selection cannot be observed; repair the settings before publication; no settings were changed"
		input.Reason = mcpobserve.ReasonHostSettingsUnobserved
		for _, cause := range []struct {
			failure error
			reason  mcpobserve.ReasonCode
		}{
			{profile.ErrPiNativeVersionUnqualified, mcpobserve.ReasonHostVersionUnqualified},
			{profile.ErrPiNativeBuiltinDisabled, mcpobserve.ReasonHostBuiltinDisabled},
			{profile.ErrPiNativeBuiltinUnobserved, mcpobserve.ReasonHostBuiltinUnobserved},
			{profile.ErrPiNativeAdapterConfigured, mcpobserve.ReasonHostAdapterConfigured},
		} {
			if errors.Is(err, cause.failure) {
				input.Reason = cause.reason
				input.Detail = cause.failure.Error()
				break
			}
		}
	}
	return mcpobserve.NewHostPrerequisiteObservation(input)
}

func observePiNativeSettings(ctx context.Context, path string) (piNativeSettings, error) {
	content, exists, err := filesnapshot.ReadRegularFileContext(ctx, path, maximumConfigBytes)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return piNativeSettings{}, ctx.Err()
		}
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
		settings.extensionEntries, err = nativeSettingsStringArray(raw)
		if err != nil {
			return piNativeSettings{}, fmt.Errorf("native Pi MCP extension selection must be a string array")
		}
		for _, entry := range settings.extensionEntries {
			// Patterns filter discovered resources; they do not discover paths.
			if strings.HasPrefix(entry, "!") || strings.HasPrefix(entry, "+") || strings.HasPrefix(entry, "-") || strings.ContainsAny(entry, "*?") {
				continue
			}

			names, err := observeNativeLocalPathPackageNames(ctx, entry, filepath.Dir(path))
			if err != nil {
				return piNativeSettings{}, err
			}
			if slices.Contains(names, "pi-mcp-adapter") {
				settings.adapterDeclared = true
			}
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
				settings.adapterDeclared = true
			} else {
				names, err := observeNativeLocalPackageNames(ctx, source, filepath.Dir(path))
				if err != nil {
					return piNativeSettings{}, err
				}
				if slices.Contains(names, "pi-mcp-adapter") {
					settings.adapterDeclared = true
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
