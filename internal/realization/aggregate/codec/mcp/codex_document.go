package mcpcodec

import (
	"sort"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/output"
)

func (config *codexProjectMCPConfig) setServer(serverID string, value map[string]any) {
	config.servers[serverID] = value
	config.changedServers[serverID] = struct{}{}
}

func (config *codexProjectMCPConfig) removeServer(serverID string) {
	delete(config.servers, serverID)
	config.changedServers[serverID] = struct{}{}
}

func (config codexProjectMCPConfig) encode() ([]byte, error) {
	content, _, err := config.encodePreservingMCPParent(true)
	return content, err
}

func (config codexProjectMCPConfig) encodePreservingMCPParent(parentExistedBefore bool) ([]byte, bool, error) {
	if err := config.ctx.Err(); err != nil {
		return nil, false, err
	}
	limits := configdocument.Limits{MaximumBytes: maximumDocumentBytes, MaximumDepth: tomlstrict.MaximumDepth}
	document, err := configdocument.Absent(configdocument.TOML, limits)
	if config.source != nil {
		document, err = configdocument.Parse(config.ctx, config.source, configdocument.TOML, limits)
	}
	if err != nil {
		return nil, false, config.documentEncodingError(err)
	}
	changes, err := config.documentChanges(parentExistedBefore, limits)
	if err != nil {
		return nil, false, config.documentEncodingError(err)
	}
	updated, err := configdocument.Edit(config.ctx, document, changes)
	if err != nil {
		return nil, false, config.documentEncodingError(err)
	}

	leaves, err := updated.Leaves()
	if err != nil {
		return nil, false, config.documentEncodingError(err)
	}
	if err := config.ctx.Err(); err != nil {
		return nil, false, err
	}
	if len(leaves) == 0 && len(updated.Comments()) == 0 && !parentExistedBefore {
		return nil, false, nil
	}
	return updated.Bytes(), true, nil
}

func (config codexProjectMCPConfig) documentChanges(parentExistedBefore bool, limits configdocument.Limits) ([]configdocument.Change, error) {
	parent, err := output.NewKeyPath(codexProjectMCPManagedField)
	if err != nil {
		return nil, err
	}
	if len(config.servers) == 0 && !parentExistedBefore {
		change, err := configdocument.Remove(parent)
		return []configdocument.Change{change}, err
	}

	selected := make(map[string]any)
	serverIDs := make([]string, 0, len(config.changedServers))
	for serverID := range config.changedServers {
		serverIDs = append(serverIDs, serverID)
		if value, present := config.servers[serverID]; present {
			selected[serverID] = value
		}
	}
	sort.Strings(serverIDs)
	content, err := encodeCodexTOMLValue(map[string]any{codexProjectMCPManagedField: selected}, "", "render selected Codex MCP entries")
	if err != nil {
		return nil, err
	}
	desired, err := configdocument.Parse(config.ctx, content, configdocument.TOML, limits)
	if err != nil {
		return nil, err
	}
	if len(config.servers) == 0 {
		value, _, err := desired.Lookup(parent)
		if err != nil {
			return nil, err
		}
		change, err := configdocument.Set(parent, value)
		return []configdocument.Change{change}, err
	}

	changes := make([]configdocument.Change, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		path, err := parent.Append(serverID)
		if err != nil {
			return nil, err
		}
		value, present, err := desired.Lookup(path)
		if err != nil {
			return nil, err
		}
		var change configdocument.Change
		if present {
			change, err = configdocument.Set(path, value)
		} else {
			change, err = configdocument.Remove(path)
		}
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func (config codexProjectMCPConfig) documentEncodingError(err error) error {
	if contextErr := config.ctx.Err(); contextErr != nil {
		return contextErr
	}
	return canonicalCodexTOMLError("", "encode Codex MCP host document", err)
}
