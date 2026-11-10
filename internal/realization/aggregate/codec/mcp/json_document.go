package mcpcodec

import (
	"bytes"
	"context"
	"slices"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

func standardMCPJSON(ctx context.Context, content []byte, spec mcpConfigSpec) ([]byte, error) {
	if spec.syntax != configdocument.JSONC {
		return content, nil
	}
	limits := configdocument.Limits{MaximumBytes: maximumDocumentBytes, MaximumDepth: maximumMCPJSONDepth}
	standard, err := configdocument.StandardJSON(ctx, content, spec.syntax, limits)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, mcpJSONHostDocumentError(spec, err)
	}
	return standard, nil
}

func (config mcpConfig) editExisting(plan mcpJSONConfigEncodingPlan) ([]byte, bool, error) {
	ctx := context.Background()
	limits := configdocument.Limits{MaximumBytes: maximumDocumentBytes, MaximumDepth: maximumMCPJSONDepth}
	document, err := configdocument.Parse(ctx, config.original, config.spec.syntax, limits)
	if err != nil {
		return nil, false, mcpJSONHostDocumentError(config.spec, err)
	}
	parent, err := output.NewKeyPath(config.spec.serversKey)
	if err != nil {
		return nil, false, err
	}
	var changes []configdocument.Change
	if !plan.includeMCPParent {
		change, err := configdocument.Remove(parent)
		if err != nil {
			return nil, false, err
		}
		changes = append(changes, change)
	} else {
		changes, err = config.serverChanges(ctx, limits)
		if err != nil {
			return nil, false, err
		}
		if _, present := config.top[config.spec.serversKey]; !present && len(config.servers) == 0 {
			change, err := mcpJSONEntryChange(ctx, limits, parent, []byte("{}"))
			if err != nil {
				return nil, false, err
			}
			changes = append(changes, change)
		}
	}
	updated, err := configdocument.Edit(ctx, document, changes)
	if err != nil {
		return nil, false, canonicalMCPJSONError("", "edit MCP JSON document", err)
	}
	if !plan.includeMCPParent && !updated.HasValues() && len(updated.Comments()) == 0 {
		return nil, false, nil
	}
	return updated.Bytes(), true, nil
}

func (config mcpConfig) serverChanges(ctx context.Context, limits configdocument.Limits) ([]configdocument.Change, error) {
	names := make([]string, 0, len(config.beforeServers)+len(config.servers))
	for name := range config.beforeServers {
		names = append(names, name)
	}
	for name := range config.servers {
		names = append(names, name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	var changes []configdocument.Change
	for _, name := range names {
		before, existed := config.beforeServers[name]
		after, present := config.servers[name]
		if existed == present && bytes.Equal(before, after) {
			continue
		}
		path, err := output.NewKeyPath(config.spec.serversKey, name)
		if err != nil {
			return nil, err
		}
		change, err := mcpJSONEntryChange(ctx, limits, path, after)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	return changes, nil
}

func mcpJSONEntryChange(ctx context.Context, limits configdocument.Limits, path output.KeyPath, raw []byte) (configdocument.Change, error) {
	if raw == nil {
		return configdocument.Remove(path)
	}
	document, err := configdocument.Parse(ctx, raw, configdocument.JSON, limits)
	if err != nil {
		return configdocument.Change{}, err
	}
	value, _, err := document.Lookup(output.KeyPath{})
	if err != nil {
		return configdocument.Change{}, err
	}
	return configdocument.Set(path, value)
}
