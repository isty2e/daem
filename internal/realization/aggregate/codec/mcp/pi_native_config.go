package mcpcodec

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

type piNativeServerEntry struct {
	Command  string            `json:"command"`
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	Enabled  bool              `json:"enabled"`
	Exposure string            `json:"exposure"`
}

func canonicalPiNativeServerEntry(serverID string, command string, args []string, env map[string]string) ([]byte, error) {
	entry := piNativeServerEntry{Command: command, Args: append([]string{}, args...), Env: env, Enabled: true, Exposure: "codemode"}
	if _, err := canonicalJSONEncodedSize(entry); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	canonical, err := decodePiNativeServerEntry(raw, serverID)
	if err != nil {
		return nil, err
	}
	return encodeMCPJSONServerEntry(canonical, serverID, mcpConfigSpecForPlacement(aggregate.MCPPlacementPiProject, "native Pi MCP", mcpManagedServersField))
}

func decodePiNativeServerEntry(raw json.RawMessage, serverID string) (piNativeServerEntry, error) {
	if _, err := aggregate.PiNativeMCPNamespace(serverID); err != nil {
		return piNativeServerEntry{}, fmt.Errorf("native Pi MCP server name must contain only letters, digits, underscores and hyphens")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return piNativeServerEntry{}, fmt.Errorf("native Pi MCP server entry must be a JSON object")
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		switch name {
		case "command", "args", "env", "enabled", "exposure", "type":
		default:
			return piNativeServerEntry{}, fmt.Errorf("unsupported managed native Pi MCP field %q", name)
		}
	}

	entry := piNativeServerEntry{Args: []string{}, Env: map[string]string{}, Enabled: true, Exposure: "codemode"}
	if err := decodeRequiredString(fields, "command", serverID, &entry.Command); err != nil {
		return piNativeServerEntry{}, err
	}
	if err := validateMCPCommand(entry.Command); err != nil {
		return piNativeServerEntry{}, err
	}
	if rawArgs, present := fields["args"]; present {
		if err := json.Unmarshal(rawArgs, &entry.Args); err != nil || entry.Args == nil {
			return piNativeServerEntry{}, fmt.Errorf("native Pi MCP args must be a JSON string array")
		}
	}
	if rawEnv, present := fields["env"]; present {
		if err := json.Unmarshal(rawEnv, &entry.Env); err != nil || entry.Env == nil {
			return piNativeServerEntry{}, fmt.Errorf("native Pi MCP env must be a JSON string map")
		}
	}
	env, err := canonicalMCPEnv(entry.Env)
	if err != nil {
		return piNativeServerEntry{}, err
	}
	entry.Env = env
	if rawEnabled, present := fields["enabled"]; present {
		if string(rawEnabled) != "true" {
			return piNativeServerEntry{}, fmt.Errorf("managed native Pi MCP requires enabled = true")
		}
	}
	if rawExposure, present := fields["exposure"]; present {
		if err := json.Unmarshal(rawExposure, &entry.Exposure); err != nil {
			return piNativeServerEntry{}, fmt.Errorf("native Pi MCP exposure must be codemode")
		}
		if entry.Exposure == "codemode-deferred" {
			entry.Exposure = "codemode"
		}
		if entry.Exposure != "codemode" {
			return piNativeServerEntry{}, fmt.Errorf("managed native Pi MCP requires codemode exposure")
		}
	}
	if rawType, present := fields["type"]; present {
		var transport string
		if err := json.Unmarshal(rawType, &transport); err != nil || transport != "stdio" {
			return piNativeServerEntry{}, fmt.Errorf("managed native Pi MCP requires stdio transport")
		}
	}
	return entry, nil
}

func piNativeServerEntriesEqual(left piNativeServerEntry, right piNativeServerEntry) bool {
	return left.Command == right.Command && slices.Equal(left.Args, right.Args) &&
		maps.Equal(left.Env, right.Env) && left.Enabled == right.Enabled && left.Exposure == right.Exposure
}

func admitPiNativeDocument(content []byte, spec mcpConfigSpec) error {
	if err := jsonstrict.Validate(content, "native Pi MCP config JSON", maximumMCPJSONDepth); err != nil {
		return mcpJSONHostDocumentError(spec, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil || top == nil {
		return fmt.Errorf("native Pi MCP config must be a JSON object")
	}
	var servers map[string]json.RawMessage
	if raw, present := top["mcpServers"]; present {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return fmt.Errorf("native Pi mcpServers must be a JSON object")
		}
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		if _, err := aggregate.PiNativeMCPNamespace(name); err == nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return aggregate.AdmitPiNativeMCPNamespaces(names)
}

func newPiNativePlacementOperations(placement aggregate.MCPPlacement) (MCPPlacementOperations, error) {
	spec := mcpConfigSpecForPlacement(placement.ID(), "native Pi MCP", mcpManagedServersField).
		withDocumentAdmission(admitPiNativeDocument)
	decode := decodePiNativeServerEntry
	return newMCPPlacementOperations(mcpPlacementOperationsInput{
		placement: placement,
		foldMutations: func(content []byte, mutations []MCPProjectionMutation) ([]byte, error) {
			return foldMCPJSONServerMutations(content, mutations, spec, decode)
		},
		restoreMutations: func(content []byte, mutations []MCPProjectionMutation, parent bool) ([]byte, bool, error) {
			return restoreMCPJSONServerMutations(content, mutations, parent, spec, decode)
		},
		verifyMutations: func(content []byte, mutations []MCPProjectionMutation) error {
			return verifyMCPJSONServerMutations(content, mutations, spec, decode, piNativeServerEntriesEqual)
		},
		observeCanonical: func(content []byte, names []string) (MCPProjectionObservation, error) {
			return observeMCPJSONServerProjections(content, names, spec, decode)
		},
		mergeCanonicalEntry: func(content []byte, name string, canonical []byte) ([]byte, error) {
			return mergeMCPJSONServerCanonicalEntry(content, name, canonical, spec, decode)
		},
		removeProjection: func(content []byte, name string) ([]byte, error) {
			return removeMCPJSONServerProjection(content, name, spec, decode)
		},
		restoreRemove: func(content []byte, name string, parent bool) ([]byte, bool, error) {
			return restoreRemoveMCPJSONServerProjection(content, name, parent, spec, decode)
		},
		extractCanonicalEntry: func(content []byte, name string) ([]byte, bool, error) {
			return extractMCPJSONServerProjectionBytes(content, name, spec, decode)
		},
		compareCanonicalEntry: func(content []byte, name string, canonical []byte) (MCPProjectionCanonicalComparison, error) {
			desired, err := decodeCanonicalMCPJSONServerEntry(canonical, name, spec, decode)
			if err != nil {
				return MCPProjectionCanonicalComparison{}, err
			}
			actual, present, err := extractMCPJSONServerProjection(content, name, spec, decode)
			if err != nil {
				return MCPProjectionCanonicalComparison{}, err
			}
			return MCPProjectionCanonicalComparison{
				ContentPath: piMCPContentPath(placement.ID(), name), Present: present,
				Equivalent: present && piNativeServerEntriesEqual(actual, desired),
			}, nil
		},
		entryPresent: func(content []byte, name string) (bool, error) {
			return mcpJSONServerEntryPresent(content, name, spec)
		},
		parentPresent: func(content []byte) (bool, error) {
			return mcpJSONServersParentPresent(content, spec)
		},
	})
}
