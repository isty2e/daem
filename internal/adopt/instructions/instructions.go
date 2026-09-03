package instructions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/filesnapshot"
	hostsurfacecatalog "github.com/isty2e/daem/internal/hostsurface/catalog"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/source/directfile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	importInstructionSkipMissing        = "missing"
	importInstructionSkipEmpty          = "empty_instruction_file"
	importInstructionSkipNotImplemented = "instruction_import_not_implemented"
	importInstructionSkipClassifyOnly   = "instruction_classify_only"
	importInstructionSkipPolicy         = "instruction_import_skipped_by_policy"
	importInstructionSkipSymlink        = "instruction_final_symlink"
	importInstructionSkipNotRegular     = "instruction_not_regular_file"
	importInstructionSkipTooLarge       = "instruction_file_too_large"
	importInstructionSkipChanged        = "instruction_file_changed_during_read"
)

const importInstructionSourceDirectoryName = "instructions"

var instructionNameSegmentPattern = regexp.MustCompile(`[^a-z0-9]+`)

type instructionImportSpec struct {
	Target       targetpkg.Target
	Scope        targetpkg.Scope
	LivePath     string
	ResourceName string
	SourcePath   string
	RenderTo     string
}

func Candidates(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
	skipped adopt.SkipEmitter,
) ([]adopt.Source, error) {
	if ctx == nil {
		return nil, fmt.Errorf("instruction import context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	compiled := hostsurfacecatalog.Product()
	locations := compiled.ManagedPathDiscoveryLocations(target, scope, entity.KindInstructions)
	if len(locations) == 0 {
		if err := skipped.Add(unsupportedInstructionImportSkip(target, scope)); err != nil {
			return nil, err
		}
		return nil, nil
	}

	defaultSpecs := make([]instructionImportSpec, 0, len(locations))
	alternatePlacementSpecs := make([]instructionImportSpec, 0)
	for _, location := range locations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		livePath, err := instructionLocationPath(location.Path())
		if err != nil {
			return nil, err
		}
		switch location.ImportPolicy() {
		case profile.ImportPolicyClassify:
			if skip, ok, err := classifyOnlyInstructionSkip(livePath); err != nil {
				return nil, err
			} else if ok {
				if err := skipped.Add(skip); err != nil {
					return nil, err
				}
			}
		case profile.ImportPolicyInclude:
			spec, err := instructionImportSpecForLocation(target, scope, location, livePath)
			if err != nil {
				return nil, err
			}
			view, isPlacement := compiled.ManagedPathAt(
				target,
				scope,
				entity.KindInstructions,
				location.Path(),
			)
			if isPlacement && !view.IsDefaultPlacement() {
				alternatePlacementSpecs = append(alternatePlacementSpecs, spec)
				continue
			}
			defaultSpecs = append(defaultSpecs, spec)
		default:
			return nil, fmt.Errorf("unsupported instruction import policy %q for %s", location.ImportPolicy(), livePath)
		}
	}
	for _, location := range compiled.ManagedPathRuntimeLocations(target, scope, entity.KindInstructions) {
		livePath, err := instructionLocationPath(location.Path())
		if err != nil {
			return nil, err
		}
		if skip, ok, err := classifyOnlyInstructionSkip(livePath); err != nil {
			return nil, err
		} else if ok {
			if err := skipped.Add(skip); err != nil {
				return nil, err
			}
		}
	}

	sources := make([]adopt.Source, 0, 1+len(alternatePlacementSpecs))
	source, ok, err := firstImportableInstructionSource(ctx, sourceDirectory, defaultSpecs, skipped)
	if err != nil {
		return nil, err
	}
	if ok {
		sources = append(sources, source)
	}
	for _, spec := range alternatePlacementSpecs {
		source, skip, err := importInstructionFileCandidate(ctx, sourceDirectory, spec)
		if err != nil {
			return nil, err
		}
		if skip.Reason != "" {
			if err := skipped.Add(skip); err != nil {
				return nil, err
			}
			continue
		}
		sources = append(sources, source)
	}

	return sources, nil
}

func importInstructionFileCandidate(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	spec instructionImportSpec,
) (adopt.Source, adopt.Skipped, error) {
	content, skip, err := readInstructionImportContent(ctx, spec.LivePath)
	if err != nil || skip.Reason != "" {
		return adopt.Source{}, skip, err
	}
	source, err := newInstructionImportSource(sourceDirectory, spec, content)
	return source, adopt.Skipped{}, err
}

func firstImportableInstructionSource(
	ctx context.Context,
	sourceDirectory adopt.SourceDirectory,
	specs []instructionImportSpec,
	skipped adopt.SkipEmitter,
) (adopt.Source, bool, error) {
	for _, spec := range specs {
		if err := ctx.Err(); err != nil {
			return adopt.Source{}, false, err
		}
		source, skip, err := importInstructionFileCandidate(ctx, sourceDirectory, spec)
		if err != nil {
			return adopt.Source{}, false, err
		}
		if skip.Reason != "" {
			if err := skipped.Add(skip); err != nil {
				return adopt.Source{}, false, err
			}
			continue
		}
		return source, true, nil
	}
	return adopt.Source{}, false, nil
}

func unsupportedInstructionImportSkip(target targetpkg.Target, scope targetpkg.Scope) adopt.Skipped {
	return adopt.Skipped{
		LivePath: fmt.Sprintf("%s:%s:instructions", target, scope),
		Reason:   importInstructionSkipNotImplemented,
	}
}

func readInstructionImportContent(ctx context.Context, livePath string) ([]byte, adopt.Skipped, error) {
	content, exists, err := filesnapshot.ReadRegularFileContext(ctx, livePath, directfile.MaximumBytes)
	if err != nil {
		if skip, ok := instructionSnapshotSkip(livePath, err); ok {
			return nil, skip, nil
		}
		return nil, adopt.Skipped{}, fmt.Errorf("read live path %q: %w", livePath, err)
	}
	if !exists {
		return nil, adopt.Skipped{LivePath: livePath, Reason: importInstructionSkipMissing}, nil
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, adopt.Skipped{LivePath: livePath, Reason: importInstructionSkipEmpty}, nil
	}
	return content, adopt.Skipped{}, nil
}

func instructionSnapshotSkip(livePath string, err error) (adopt.Skipped, bool) {
	var reason adopt.SkipReason
	switch {
	case errors.Is(err, filesnapshot.ErrSymlink):
		reason = importInstructionSkipSymlink
	case errors.Is(err, filesnapshot.ErrNotRegular):
		reason = importInstructionSkipNotRegular
	case errors.Is(err, filesnapshot.ErrLimitExceeded):
		reason = importInstructionSkipTooLarge
	case errors.Is(err, filesnapshot.ErrChanged):
		reason = importInstructionSkipChanged
	default:
		return adopt.Skipped{}, false
	}
	return adopt.Skipped{LivePath: livePath, Reason: reason}, true
}

func newInstructionImportSource(
	sourceDirectory adopt.SourceDirectory,
	spec instructionImportSpec,
	content []byte,
) (adopt.Source, error) {
	sourcePath, err := sourceDirectory.Resolve(spec.SourcePath)
	if err != nil {
		return adopt.Source{}, err
	}

	return adopt.Source{
		ResourceName: spec.ResourceName,
		Target:       spec.Target,
		Scope:        spec.Scope,
		LivePath:     spec.LivePath,
		SourcePath:   sourcePath,
		RenderTo:     spec.RenderTo,
		Content:      content,
	}, nil
}

func instructionImportSpecForLocation(
	target targetpkg.Target,
	scope targetpkg.Scope,
	location profile.DiscoveryLocation,
	livePath string,
) (instructionImportSpec, error) {
	spec := instructionImportSpec{
		Target:       target,
		Scope:        scope,
		LivePath:     livePath,
		ResourceName: defaultInstructionResourceName(target, scope),
		SourcePath:   defaultInstructionSourcePath(target, scope),
	}
	view, isPlacement := hostsurfacecatalog.Product().ManagedPathAt(
		target,
		scope,
		entity.KindInstructions,
		location.Path(),
	)
	if !isPlacement || view.IsDefaultPlacement() {
		return spec, nil
	}

	renderTo, err := renderToForInstructionPlacement(location)
	if err != nil {
		return instructionImportSpec{}, err
	}
	suffix := instructionLocationNameSuffix(location.Path())
	spec.ResourceName += "_" + suffix
	spec.SourcePath = instructionSourcePath(target, scope, suffix)
	spec.RenderTo = renderTo
	return spec, nil
}

func defaultInstructionResourceName(target targetpkg.Target, scope targetpkg.Scope) string {
	return instructionTargetResourceSegment(target) + "_" + string(scope)
}

func defaultInstructionSourcePath(target targetpkg.Target, scope targetpkg.Scope) string {
	return instructionSourcePath(target, scope, "")
}

func instructionSourcePath(target targetpkg.Target, scope targetpkg.Scope, suffix string) string {
	name := string(target) + "-" + string(scope)
	if suffix != "" {
		name += "-" + strings.ReplaceAll(suffix, "_", "-")
	}
	return filepath.ToSlash(filepath.Join(importInstructionSourceDirectoryName, name+".md"))
}

func instructionTargetResourceSegment(target targetpkg.Target) string {
	return strings.ReplaceAll(string(target), "-", "_")
}

func instructionLocationNameSuffix(path string) string {
	normalized := strings.ToLower(strings.TrimSpace(filepath.ToSlash(path)))
	normalized = strings.TrimPrefix(normalized, "~/")
	normalized = strings.TrimSuffix(normalized, ".md")
	normalized = instructionNameSegmentPattern.ReplaceAllString(normalized, "_")
	normalized = strings.Trim(normalized, "_")
	if normalized == "" {
		return "path"
	}
	return normalized
}

func renderToForInstructionPlacement(location profile.DiscoveryLocation) (string, error) {
	if strings.HasPrefix(location.Path(), "~/") || filepath.IsAbs(location.Path()) {
		return "", fmt.Errorf("instruction import cannot preserve non-default global placement path %q", location.Path())
	}
	return filepath.ToSlash(location.Path()), nil
}

func classifyOnlyInstructionSkip(livePath string) (adopt.Skipped, bool, error) {
	_, err := os.Lstat(livePath)
	if os.IsNotExist(err) {
		return adopt.Skipped{}, false, nil
	}
	if err != nil {
		return adopt.Skipped{}, false, fmt.Errorf("inspect instruction path %q: %w", livePath, err)
	}
	return adopt.Skipped{LivePath: livePath, Reason: importInstructionSkipClassifyOnly}, true, nil
}

func instructionLocationPath(locationPath string) (string, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome != "" && strings.HasPrefix(locationPath, "~/.codex/") {
		return filepath.Join(codexHome, filepath.FromSlash(strings.TrimPrefix(locationPath, "~/.codex/"))), nil
	}
	if strings.HasPrefix(locationPath, "~/") {
		homeDirectory, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return filepath.Join(homeDirectory, filepath.FromSlash(strings.TrimPrefix(locationPath, "~/"))), nil
	}
	if filepath.IsAbs(locationPath) {
		return filepath.Clean(locationPath), nil
	}
	return filepath.FromSlash(locationPath), nil
}
