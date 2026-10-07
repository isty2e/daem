package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology/mcp"
)

// PiNativeInput observes the conditional two-file native envelope, not an activated Pi session.
type PiNativeInput struct {
	Projection   aggregate.SubjectContribution
	Codecs       aggregate.CodecCatalog
	WorkDir      string
	AgentRoot    string
	SelectedPath string
	Retiring     bool
}

// ObservePiNative keeps native replacement/override semantics separate from adapter imports and lazy lifecycle.
func ObservePiNative(input PiNativeInput) (mcpeffective.Observation, error) {
	contribution := input.Projection.Contribution()
	if contribution.CodecContractID() != aggregate.MCPCodecPiNativeStdio {
		return mcpeffective.Observation{}, fmt.Errorf("native Pi observation requires the recorded native codec")
	}
	codec, ok := input.Codecs.Lookup(contribution.CodecContractID())
	if !ok {
		return mcpeffective.Observation{}, fmt.Errorf("native Pi codec is unavailable")
	}
	name, ok := mcp.ServerID(input.Projection.SubjectID())
	if !ok {
		return mcpeffective.Observation{}, fmt.Errorf("native Pi projection has no server identity")
	}
	selection, err := aggregate.NewSelection([]aggregate.ProjectionContract{contribution.Contract()})
	if err != nil {
		return mcpeffective.Observation{}, err
	}
	globalPath := filepath.Join(input.AgentRoot, "mcp.json")
	projectPath := filepath.Join(input.WorkDir, ".pi", "mcp.json")
	if input.SelectedPath != globalPath && input.SelectedPath != projectPath {
		return mcpeffective.Observation{}, fmt.Errorf("managed native Pi path differs from the active agent/project root")
	}
	specs := []normalSourceSpec{{id: "pi-native-global", path: globalPath, shared: true}, {id: "pi-native-project", path: projectPath}}
	selectedIndex := 0
	if input.SelectedPath == projectPath {
		selectedIndex = 1
	}
	sources := make([]mcpeffective.SourceObservation, 0, len(specs))
	for index, spec := range specs {
		precedence := relativePrecedence(index, selectedIndex)
		content, exists, readErr := filesnapshot.ReadRegularFile(spec.path, maximumConfigBytes)
		source := mcpeffective.SourceObservationInput{
			ID: spec.id, Path: spec.path, Kind: mcpeffective.SourceNormal, Precedence: precedence,
			Shared: spec.shared, State: mcpeffective.SourceAbsent,
			DefinitionEquivalence: mcpeffective.DefinitionEquivalenceNotApplicable,
		}
		if readErr != nil {
			source.State = mcpeffective.SourceOpaque
			source.Detail = "native Pi MCP config cannot be read as a bounded regular file"
		} else if exists {
			native, decodeErr := decodeNativeConfig(content, name)
			if decodeErr != nil {
				source.State = mcpeffective.SourceOpaque
				source.Detail = decodeErr.Error()
			} else {
				source.State = mcpeffective.SourceExact
				if entry, defines := native[name]; defines {
					source.DefinesSelectedName = true
					source.DefinitionEquivalence = mcpeffective.DefinitionEquivalenceUnknown
					comparison := content
					if index == 1 && selectedIndex == 0 {
						base := []byte(contribution.CanonicalContribution())
						if input.Retiring {
							base = nil
						}
						merged, override, mergeErr := nativeProjectDefinition(entry, base)
						if mergeErr != nil {
							source.State = mcpeffective.SourceOpaque
							source.DefinesSelectedName = false
							source.DefinitionEquivalence = mcpeffective.DefinitionEquivalenceNotApplicable
							source.Detail = mergeErr.Error()
						} else if override && input.Retiring {
							source.DefinesSelectedName = false
							source.DefinitionEquivalence = mcpeffective.DefinitionEquivalenceNotApplicable
							source.DependsOnRemovedDefinition = true
						} else {
							comparison, _ = json.Marshal(map[string]json.RawMessage{"mcpServers": mustNativeServerMap(name, merged)})
						}
					}
					if source.State == mcpeffective.SourceExact && source.DefinesSelectedName {
						source.DefinitionEquivalence = compareNormalDefinition(comparison, contribution, selection, codec)
					}
				}
			}
		}
		sources = append(sources, mustSourceObservation(source))
	}
	policy := mcpeffective.PolicyReplaceLower
	if input.Retiring {
		policy = mcpeffective.PolicyExclusive
	}
	return mcpeffective.NewObservation(mcpeffective.ObservationInput{
		Subject: input.Projection.SubjectID(), ServerName: name, SelectedPath: input.SelectedPath, Sources: sources,
		Policy: policy,
	})
}

func mustNativeServerMap(name string, entry json.RawMessage) json.RawMessage {
	content, _ := json.Marshal(map[string]json.RawMessage{name: entry})
	return content
}

func decodeNativeConfig(content []byte, selectedName string) (map[string]json.RawMessage, error) {
	if err := jsonstrict.Validate(content, "native Pi MCP config", maximumConfigDepth); err != nil {
		return nil, fmt.Errorf("native Pi MCP config is not strict unambiguous JSON")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil || top == nil {
		return nil, fmt.Errorf("native Pi MCP config must be an object")
	}
	servers := map[string]json.RawMessage{}
	if raw, exists := top["mcpServers"]; exists {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return nil, fmt.Errorf("native Pi mcpServers must be an object")
		}
	}
	namespace, err := aggregate.PiNativeMCPNamespace(selectedName)
	if err != nil {
		return nil, err
	}
	for name := range servers {
		other, err := aggregate.PiNativeMCPNamespace(name)
		if err == nil && name != selectedName && other == namespace {
			return nil, fmt.Errorf("native Pi MCP namespace conflicts with the selected server")
		}
	}
	return servers, nil
}

func nativeProjectDefinition(project json.RawMessage, global json.RawMessage) (json.RawMessage, bool, error) {
	var entry map[string]json.RawMessage
	if err := json.Unmarshal(project, &entry); err != nil || entry == nil {
		return nil, false, fmt.Errorf("native Pi project entry is not an object")
	}
	for _, field := range []string{"command", "url", "type"} {
		if _, exists := entry[field]; exists {
			return project, false, nil
		}
	}
	for field, value := range entry {
		switch field {
		case "enabled":
			if !bytes.Equal(value, []byte("true")) && !bytes.Equal(value, []byte("false")) {
				return nil, false, fmt.Errorf("native Pi override enabled must be boolean")
			}
		case "exposure":
			var exposure string
			if err := json.Unmarshal(value, &exposure); err != nil {
				return nil, false, fmt.Errorf("native Pi override exposure must be a string")
			}
		case "toolExposure":
			// Per-tool exposure is outside the managed native contribution contract.
		default:
			return nil, false, fmt.Errorf("native Pi partial overrides may set only enabled, exposure and toolExposure")
		}
	}
	if len(global) == 0 {
		return nil, true, nil
	}
	var base map[string]json.RawMessage
	if err := json.Unmarshal(global, &base); err != nil || base == nil {
		return nil, false, fmt.Errorf("native Pi override has no comparable global definition")
	}
	for field, value := range entry {
		base[field] = value
	}
	merged, err := json.Marshal(base)
	return merged, true, err
}
