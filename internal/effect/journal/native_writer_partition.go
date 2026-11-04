package journal

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type documentWriterPath struct {
	volume     string
	components []string
	group      int
}

// Metadata partitioning does not qualify current roots or replace alias leases.
func (record nativeJournalRecord) validateWriterSpellingPartition(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("document writer partition context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(record.DocumentGroups) == 0 {
		return nil
	}
	var paths []documentWriterPath
	appendPath := func(path string, group int) error {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsRune(path, '\x00') {
			return fmt.Errorf("document writer partition requires a canonical absolute path")
		}
		volume, components := recoveryFilesystemPathComponents(path)
		paths = append(paths, documentWriterPath{volume, components, group})
		return nil
	}
	for index, group := range record.DocumentGroups {
		if err := ctx.Err(); err != nil {
			return err
		}
		writer, err := documentRootedWriterPath(group.Writer.Root, group.Writer.RelativePath)
		if err != nil {
			return err
		}
		if err := appendPath(writer, index); err != nil {
			return err
		}
		for _, alias := range group.Aliases {
			if err := ctx.Err(); err != nil {
				return err
			}
			address, err := alias.Address.canonical()
			if err != nil {
				return err
			}
			if err := alias.LogicalRoot.validate(address.Scope(), record.ManifestRootProvenance); err != nil {
				return err
			}
			selected, err := documentRootedWriterPath(alias.LogicalRoot.Provenance, address.AggregateRoot().RelativePath())
			if err != nil {
				return err
			}
			if err := appendPath(selected, index); err != nil {
				return err
			}
		}
	}
	for _, entry := range record.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		writer, err := record.ordinaryWriterPath(entry)
		if err != nil {
			return err
		}
		// Ordinary projections retain their existing per-entry overlap grammar.
		if err := appendPath(writer, -1); err != nil {
			return err
		}
	}
	return validateDocumentWriterPaths(ctx, paths)
}

func documentRootedWriterPath(root recoveryRootProvenance, relative string) (string, error) {
	if _, err := root.canonical(); err != nil {
		return "", err
	}
	destination, err := rootedpath.NewRelativeDestination(relative)
	if err != nil || destination.Path() != relative {
		return "", fmt.Errorf("document writer partition requires a canonical relative path")
	}
	return filepath.Join(root.PhysicalRoot, filepath.FromSlash(relative)), nil
}

func (record nativeJournalRecord) ordinaryWriterPath(entry recoveryEntry) (string, error) {
	view := recordedPathEntryView(entry)
	scope, destination, err := view.WriterAddress()
	if err != nil {
		return "", err
	}
	switch scope {
	case target.ScopeProject:
		if destination.RootRole() != output.RootProject {
			return "", fmt.Errorf("project writer has a non-project destination")
		}
		return documentRootedWriterPath(record.ManifestRootProvenance, destination.RelativePath())
	case target.ScopeGlobal:
		if !view.HasGlobalBinding() {
			return "", fmt.Errorf("global writer requires its capture-time path binding")
		}
		return view.GlobalPath(), nil
	default:
		return "", fmt.Errorf("document writer partition has an unsupported ordinary scope")
	}
}

func validateDocumentWriterPaths(ctx context.Context, paths []documentWriterPath) error {
	slices.SortFunc(paths, func(left, right documentWriterPath) int {
		if order := cmp.Compare(left.volume, right.volume); order != 0 {
			return order
		}
		return slices.Compare(left.components, right.components)
	})
	var covering *documentWriterPath
	for index := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		current := &paths[index]
		if covering != nil && covering.volume == current.volume && len(covering.components) <= len(current.components) && slices.Equal(covering.components, current.components[:len(covering.components)]) {
			if covering.group != current.group {
				return fmt.Errorf("native document writers or selected alias routes overlap another group or ordinary entry")
			}
			continue
		}
		covering = current
	}
	return ctx.Err()
}
