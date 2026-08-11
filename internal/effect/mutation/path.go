package mutation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
)

type pathCaseSemantics uint8

const (
	pathCaseSensitive pathCaseSemantics = iota + 1
	pathCaseInsensitive
)

func (semantics pathCaseSemantics) validate() error {
	switch semantics {
	case pathCaseSensitive, pathCaseInsensitive:
		return nil
	default:
		return fmt.Errorf("unsupported path case semantics %d", semantics)
	}
}

func (semantics pathCaseSemantics) canonicalComponent(spelling string) (string, error) {
	if err := semantics.validate(); err != nil {
		return "", err
	}
	if spelling == "" || spelling == "." || spelling == ".." ||
		strings.ContainsRune(spelling, filepath.Separator) {
		return "", fmt.Errorf("invalid observed path component %q", spelling)
	}
	if semantics == pathCaseInsensitive {
		return strings.ToLower(spelling), nil
	}
	return spelling, nil
}

type observedPathComponent struct {
	spelling string
	caseMode pathCaseSemantics
}

type pathSemanticsWitness string

func newPathSemanticsWitness(platform string, components []observedPathComponent) (pathSemanticsWitness, error) {
	if platform == "" {
		return "", fmt.Errorf("path semantics witness platform is required")
	}
	var builder strings.Builder
	builder.WriteString(platform)
	builder.WriteByte(':')
	for _, component := range components {
		if err := component.caseMode.validate(); err != nil {
			return "", err
		}
		switch component.caseMode {
		case pathCaseSensitive:
			builder.WriteByte('s')
		case pathCaseInsensitive:
			builder.WriteByte('i')
		}
	}
	return pathSemanticsWitness(builder.String()), nil
}

type canonicalPath struct {
	keyPath        string
	accessPath     string
	witness        pathSemanticsWitness
	provisional    pathauthority.Provisional
	namespaceLease namespaceLeaseIntent
}

type pathSelection struct {
	anchorPath         string
	missingComponents  []string
	finalEntryMayExist bool
}

// CanonicalDirectoryEntryPath returns the physical access path used by a
// directory-entry mutation domain. Ancestor symlinks are resolved while the
// final entry name is retained for no-follow mutation.
func CanonicalDirectoryEntryPath(path string) (string, error) {
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		return "", err
	}
	return identity.accessPath, nil
}

// CanonicalDirectoryEntryKey returns the platform-normalized comparison key
// used by directory-entry mutation domains. A missing normalization-sensitive
// Darwin entry returns a provisional candidate key, not durable authority;
// persistence callers must use ObservePersistedDirectoryEntryAuthority.
func CanonicalDirectoryEntryKey(path string) (string, error) {
	identity, err := canonicalPathIdentity(path, PathEffectDirectoryEntry)
	if err != nil {
		return "", err
	}
	return identity.keyPath, nil
}

// CanonicalDirectoryEntryKeyBounded returns the platform-normalized comparison
// key while charging every physical path observation to one operation budget.
func CanonicalDirectoryEntryKeyBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (string, error) {
	selection, err := selectDirectoryEntryPathBounded(path, maximumPhysicalDepth, budget)
	if err != nil {
		return "", err
	}
	identity, err := canonicalPathIdentityFromSelectionBounded(
		path,
		selection,
		PathEffectDirectoryEntry,
		maximumPhysicalDepth,
		budget,
	)
	if err != nil {
		return "", err
	}
	return identity.keyPath, nil
}

func canonicalPathIdentity(path string, effect PathEffect) (canonicalPath, error) {
	selection, err := selectPath(path, effect)
	if err != nil {
		return canonicalPath{}, err
	}
	return canonicalPathIdentityFromSelection(path, selection, effect)
}

func canonicalPathIdentityFromSelection(
	path string,
	selection pathSelection,
	effect PathEffect,
) (canonicalPath, error) {
	identity, err := platformCanonicalPath(selection, effect)
	return validateCanonicalPathIdentity(path, identity, err)
}

func canonicalPathIdentityFromSelectionBounded(
	path string,
	selection pathSelection,
	effect PathEffect,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (canonicalPath, error) {
	identity, err := platformCanonicalPathBounded(
		selection,
		effect,
		maximumPhysicalDepth,
		budget,
	)
	return validateCanonicalPathIdentity(path, identity, err)
}

func validateCanonicalPathIdentity(
	path string,
	identity canonicalPath,
	err error,
) (canonicalPath, error) {
	if err != nil {
		return canonicalPath{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
	}
	if identity.keyPath == "" || identity.accessPath == "" || identity.witness == "" {
		return canonicalPath{}, fmt.Errorf("canonicalize mutation path %q: platform identity is incomplete", path)
	}
	if !identity.provisional.IsZero() {
		if err := identity.provisional.Validate(); err != nil {
			return canonicalPath{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
		}
	}
	if !identity.namespaceLease.isZero() {
		if err := identity.namespaceLease.validate(); err != nil {
			return canonicalPath{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
		}
	}
	return identity, nil
}

func selectPath(path string, effect PathEffect) (pathSelection, error) {
	if err := effect.validate(); err != nil {
		return pathSelection{}, err
	}
	if strings.TrimSpace(path) == "" {
		return pathSelection{}, fmt.Errorf("mutation path is required")
	}
	if strings.ContainsRune(path, '\x00') {
		return pathSelection{}, fmt.Errorf("mutation path contains a NUL byte")
	}
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return pathSelection{}, fmt.Errorf("resolve mutation path %q: %w", path, err)
	}

	if effect == PathEffectDirectoryEntry && filepath.Dir(absolutePath) != absolutePath {
		parent, err := resolveDeepestExisting(filepath.Dir(absolutePath))
		if err != nil {
			return pathSelection{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
		}
		missing := append(parent.missingComponents, filepath.Base(absolutePath))
		return pathSelection{
			anchorPath:         parent.anchorPath,
			missingComponents:  missing,
			finalEntryMayExist: len(parent.missingComponents) == 0,
		}, nil
	}

	selection, err := resolveDeepestExisting(absolutePath)
	if err != nil {
		return pathSelection{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
	}
	return selection, nil
}

func selectDirectoryEntryPathBounded(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (pathSelection, error) {
	if strings.TrimSpace(path) == "" {
		return pathSelection{}, fmt.Errorf("mutation path is required")
	}
	if strings.ContainsRune(path, '\x00') {
		return pathSelection{}, fmt.Errorf("mutation path contains a NUL byte")
	}
	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return pathSelection{}, fmt.Errorf("resolve mutation path %q: %w", path, err)
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(
		absolutePath,
		maximumPhysicalDepth,
		budget,
	)
	if err != nil {
		return pathSelection{}, fmt.Errorf("canonicalize mutation path %q: %w", path, err)
	}
	components := strings.Split(destination.Relative().Path(), "/")
	selection := pathSelection{
		anchorPath:         destination.Root().PhysicalRoot(),
		missingComponents:  components,
		finalEntryMayExist: len(components) == 1,
	}
	if closeErr := root.Close(); closeErr != nil {
		return pathSelection{}, errors.Join(
			fmt.Errorf("close mutation path root %q", path),
			closeErr,
		)
	}
	return selection, nil
}

func admitPlatformPathTraversal(
	path string,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) error {
	if budget == nil {
		return fmt.Errorf("physical path traversal budget is required")
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path {
		return fmt.Errorf("physical path %q must be absolute and clean", path)
	}
	volumeRoot := filepath.VolumeName(clean) + string(filepath.Separator)
	relative := strings.TrimPrefix(clean, volumeRoot)
	if relative == clean {
		return fmt.Errorf("physical path %q has no rooted prefix", path)
	}
	depth := 0
	if relative != "" {
		depth = len(strings.Split(relative, string(filepath.Separator)))
	}
	if depth > maximumPhysicalDepth {
		return fmt.Errorf(
			"physical path depth %d exceeds maximum %d",
			depth,
			maximumPhysicalDepth,
		)
	}
	return budget.AdmitPathComponents(depth)
}

func resolveDeepestExisting(path string) (pathSelection, error) {
	candidate := filepath.Clean(path)
	missing := make([]string, 0)
	for {
		_, err := os.Lstat(candidate)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return pathSelection{}, err
			}
			if len(missing) != 0 {
				resolvedInfo, err := os.Stat(resolved)
				if err != nil {
					return pathSelection{}, err
				}
				if !resolvedInfo.IsDir() {
					return pathSelection{}, fmt.Errorf("existing ancestor %q is not a directory", resolved)
				}
			}
			for left, right := 0, len(missing)-1; left < right; left, right = left+1, right-1 {
				missing[left], missing[right] = missing[right], missing[left]
			}
			return pathSelection{
				anchorPath:        filepath.Clean(resolved),
				missingComponents: missing,
			}, nil
		}
		if !os.IsNotExist(err) {
			return pathSelection{}, err
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			return pathSelection{}, fmt.Errorf("no existing ancestor for %q", path)
		}
		missing = append(missing, filepath.Base(candidate))
		candidate = parent
	}
}

func selectedAccessPath(selection pathSelection) string {
	path := selection.anchorPath
	for _, component := range selection.missingComponents {
		path = filepath.Join(path, component)
	}
	return filepath.Clean(path)
}

func canonicalObservedPath(
	root string,
	components []observedPathComponent,
	platform string,
) (canonicalPath, error) {
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return canonicalPath{}, fmt.Errorf("observed path root %q must be absolute and clean", root)
	}
	accessPath := root
	keyPath := root
	for _, component := range components {
		canonical, err := component.caseMode.canonicalComponent(component.spelling)
		if err != nil {
			return canonicalPath{}, err
		}
		accessPath = filepath.Join(accessPath, component.spelling)
		keyPath = filepath.Join(keyPath, canonical)
	}
	witness, err := newPathSemanticsWitness(platform, components)
	if err != nil {
		return canonicalPath{}, err
	}
	return canonicalPath{
		keyPath:    filepath.Clean(keyPath),
		accessPath: filepath.Clean(accessPath),
		witness:    witness,
	}, nil
}

func pathContains(parent string, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}
