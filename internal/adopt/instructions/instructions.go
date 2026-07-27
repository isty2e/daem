package instructions

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

const (
	importInstructionSkipMissing        = "missing"
	importInstructionSkipEmpty          = "empty_instruction_file"
	importInstructionSkipNotImplemented = "instruction_import_not_implemented"
	importInstructionSkipClassifyOnly   = "instruction_classify_only"
	importInstructionSkipPolicy         = "instruction_import_skipped_by_policy"
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
	sourceDirectory adopt.SourceDirectory,
	target targetpkg.Target,
	scope targetpkg.Scope,
) ([]adopt.Source, []adopt.Skipped, error) {
	locations := profile.Profile(target).DiscoveryLocations(entity.KindInstructions, scope)
	if len(locations) == 0 {
		return nil, []adopt.Skipped{unsupportedInstructionImportSkip(target, scope)}, nil
	}

	defaultSpecs := make([]instructionImportSpec, 0, len(locations))
	alternatePlacementSpecs := make([]instructionImportSpec, 0)
	skipped := make([]adopt.Skipped, 0, len(locations))

	for _, location := range locations {
		livePath, err := instructionLocationPath(location.Path())
		if err != nil {
			return nil, nil, err
		}
		switch location.ImportPolicy() {
		case profile.ImportPolicyClassify:
			if skip, ok, err := classifyOnlyInstructionSkip(livePath); err != nil {
				return nil, nil, err
			} else if ok {
				skipped = append(skipped, skip)
			}
		case profile.ImportPolicyInclude:
			spec, err := instructionImportSpecForLocation(target, scope, location, livePath)
			if err != nil {
				return nil, nil, err
			}
			placement, isPlacement := profile.Profile(target).PlacementAt(entity.KindInstructions, scope, location.Path())
			if isPlacement && !placement.Default() {
				alternatePlacementSpecs = append(alternatePlacementSpecs, spec)
				continue
			}
			defaultSpecs = append(defaultSpecs, spec)
		default:
			return nil, nil, fmt.Errorf("unsupported instruction import policy %q for %s", location.ImportPolicy(), livePath)
		}
	}
	for _, location := range profile.Profile(target).RuntimeLocations(entity.KindInstructions, scope) {
		livePath, err := instructionLocationPath(location.Path())
		if err != nil {
			return nil, nil, err
		}
		if skip, ok, err := classifyOnlyInstructionSkip(livePath); err != nil {
			return nil, nil, err
		} else if ok {
			skipped = append(skipped, skip)
		}
	}

	sources := make([]adopt.Source, 0, 1+len(alternatePlacementSpecs))
	source, defaultSkipped, ok, err := firstImportableInstructionSource(sourceDirectory, defaultSpecs)
	if err != nil {
		return nil, nil, err
	}
	skipped = append(skipped, defaultSkipped...)
	if ok {
		sources = append(sources, source)
	}
	for _, spec := range alternatePlacementSpecs {
		source, skip, err := importInstructionFileCandidate(sourceDirectory, spec)
		if err != nil {
			return nil, nil, err
		}
		if skip.Reason != "" {
			skipped = append(skipped, skip)
			continue
		}
		sources = append(sources, source)
	}

	return sources, skipped, nil
}

func importInstructionFileCandidate(
	sourceDirectory adopt.SourceDirectory,
	spec instructionImportSpec,
) (adopt.Source, adopt.Skipped, error) {
	content, skip, err := readInstructionImportContent(spec.LivePath)
	if err != nil || skip.Reason != "" {
		return adopt.Source{}, skip, err
	}
	source, err := newInstructionImportSource(sourceDirectory, spec, content)
	return source, adopt.Skipped{}, err
}

func firstImportableInstructionSource(
	sourceDirectory adopt.SourceDirectory,
	specs []instructionImportSpec,
) (adopt.Source, []adopt.Skipped, bool, error) {
	skipped := make([]adopt.Skipped, 0, len(specs))
	for _, spec := range specs {
		source, skip, err := importInstructionFileCandidate(sourceDirectory, spec)
		if err != nil {
			return adopt.Source{}, nil, false, err
		}
		if skip.Reason != "" {
			skipped = append(skipped, skip)
			continue
		}
		return source, skipped, true, nil
	}
	return adopt.Source{}, skipped, false, nil
}

func unsupportedInstructionImportSkip(target targetpkg.Target, scope targetpkg.Scope) adopt.Skipped {
	return adopt.Skipped{
		LivePath: fmt.Sprintf("%s:%s:instructions", target, scope),
		Reason:   importInstructionSkipNotImplemented,
	}
}

func readInstructionImportContent(livePath string) ([]byte, adopt.Skipped, error) {
	liveInfo, err := os.Stat(livePath)
	if os.IsNotExist(err) {
		return nil, adopt.Skipped{LivePath: livePath, Reason: importInstructionSkipMissing}, nil
	}
	if err != nil {
		return nil, adopt.Skipped{}, fmt.Errorf("read live path %q: %w", livePath, err)
	}
	if liveInfo.IsDir() {
		return nil, adopt.Skipped{}, fmt.Errorf("live path %q is a directory", livePath)
	}

	content, err := os.ReadFile(livePath)
	if err != nil {
		return nil, adopt.Skipped{}, fmt.Errorf("read live path %q: %w", livePath, err)
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return nil, adopt.Skipped{LivePath: livePath, Reason: importInstructionSkipEmpty}, nil
	}
	return content, adopt.Skipped{}, nil
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
	placement, isPlacement := profile.Profile(target).PlacementAt(entity.KindInstructions, scope, location.Path())
	if !isPlacement || placement.Default() {
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
