// Package hostconfig selects physically observed native data for explicit
// source capture. Paths, ownership, publication and display stay at boundaries.
package hostconfig

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
)

type Result struct {
	before   configdocument.Document
	after    configdocument.Document
	selected []output.KeyPath
}

func (result Result) Before() configdocument.Document { return result.before }
func (result Result) After() configdocument.Document  { return result.after }
func (result Result) Selected() []output.KeyPath      { return slices.Clone(result.selected) }

// Initial expands object selectors from this host snapshot, not from defaults
// or effective configuration. Missing or conflicting selections fail together.
func Initial(ctx context.Context, host, empty configdocument.Document, selectors []output.KeyPath, maximumPathBytes int64) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("capture context is required")
	}
	if !host.Exists() || len(selectors) == 0 || empty.HasValues() {
		return Result{}, fmt.Errorf("initial capture requires an existing host document, selected keys and an empty fragment")
	}

	requested := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if selector.IsRoot() {
			return Result{}, fmt.Errorf("key capture cannot select the document root")
		}
		if _, present, err := host.Lookup(selector); err != nil {
			return Result{}, fmt.Errorf("initial capture key %q conflicts with the host document: %w", selector.String(), err)
		} else if !present {
			return Result{}, fmt.Errorf("initial capture key %q is missing", selector.String())
		}
		requested[selector.String()] = struct{}{}
	}

	var changes []configdocument.Change
	emitted := make(map[string]struct{}, len(selectors))
	for _, selector := range selectors {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if _, duplicate := emitted[selector.String()]; duplicate {
			continue
		}
		covered := false
		for parent, present := selector.Parent(); present; parent, present = parent.Parent() {
			if _, selected := requested[parent.String()]; selected {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		value, _, _ := host.Lookup(selector)
		change, err := configdocument.Set(selector, value)
		if err != nil {
			return Result{}, err
		}
		changes = append(changes, change)
		emitted[selector.String()] = struct{}{}
	}

	after, err := configdocument.EditFragment(ctx, empty, changes)
	if err != nil {
		return Result{}, err
	}
	leaves, err := after.Leaves(ctx, maximumPathBytes)
	if err != nil {
		return Result{}, err
	}
	selected := make([]output.KeyPath, 0, len(leaves))
	for _, leaf := range leaves {
		selected = append(selected, leaf.Path)
	}
	return Result{before: empty, after: after, selected: selected}, nil
}

// Update derives membership only from declared source leaves. A selector can
// narrow that set but cannot acquire newly discovered host siblings.
func Update(ctx context.Context, source, host configdocument.Document, selectors []output.KeyPath, maximumPathBytes int64) (Result, error) {
	if !source.Exists() {
		return Result{}, fmt.Errorf("capture update requires the declared source fragment")
	}

	leaves, err := source.Leaves(ctx, maximumPathBytes)
	if err != nil {
		return Result{}, err
	}
	for _, selector := range selectors {
		if selector.IsRoot() || !slices.ContainsFunc(leaves, func(leaf configdocument.Entry) bool { return selector.Contains(leaf.Path) }) {
			return Result{}, fmt.Errorf("capture key %q does not narrow declared source membership", selector.String())
		}
	}

	var selected []output.KeyPath
	var changes []configdocument.Change
	for _, leaf := range leaves {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if len(selectors) != 0 && !slices.ContainsFunc(selectors, func(selector output.KeyPath) bool { return selector.Contains(leaf.Path) }) {
			continue
		}
		value, present, err := host.Lookup(leaf.Path)
		if err != nil {
			return Result{}, fmt.Errorf("capture key %q conflicts with the host document: %w", leaf.Path.String(), err)
		}
		change, err := configdocument.Remove(leaf.Path)
		if present {
			change, err = configdocument.Set(leaf.Path, value)
		}
		if err != nil {
			return Result{}, err
		}
		selected, changes = append(selected, leaf.Path), append(changes, change)
	}

	after, err := configdocument.EditFragment(ctx, source, changes)
	if err != nil {
		return Result{}, err
	}
	if _, err := after.Leaves(ctx, maximumPathBytes); err != nil {
		return Result{}, err
	}
	return Result{before: source, after: after, selected: selected}, nil
}
