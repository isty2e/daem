package adopt

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	targetpkg "github.com/isty2e/daem/internal/target"
)

// SourceDirectory is the manifest-adjacent root for imported source material.
type SourceDirectory struct {
	root string
}

// NewSourceDirectory validates an imported-source root against its output manifest.
func NewSourceDirectory(output string, root string) (SourceDirectory, error) {
	if strings.TrimSpace(output) == "" {
		return SourceDirectory{}, fmt.Errorf("import output is required")
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return SourceDirectory{}, fmt.Errorf("import output must be an absolute clean path")
	}
	if strings.TrimSpace(root) == "" {
		return SourceDirectory{}, fmt.Errorf("source-dir is required")
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return SourceDirectory{}, fmt.Errorf("source-dir must be an absolute clean path")
	}
	if pathHasComponent(root, ".daem") {
		return SourceDirectory{}, fmt.Errorf("source-dir must not be inside .daem")
	}
	outputDirectory := filepath.Dir(output)
	if !pathWithin(root, outputDirectory) {
		return SourceDirectory{}, fmt.Errorf("source-dir must stay inside the output manifest directory")
	}
	if pathWithin(output, root) {
		return SourceDirectory{}, fmt.Errorf("output must not be inside source-dir")
	}

	return SourceDirectory{root: root}, nil
}

// Root returns the validated imported-source root.
func (directory SourceDirectory) Root() string {
	return directory.root
}

// Resolve returns a path beneath the imported-source root.
func (directory SourceDirectory) Resolve(relativePath string) (string, error) {
	if directory.root == "" {
		return "", fmt.Errorf("source-dir is not initialized")
	}
	if strings.TrimSpace(relativePath) == "" {
		return "", fmt.Errorf("import source relative path is required")
	}
	relativePath = filepath.FromSlash(relativePath)
	if filepath.IsAbs(relativePath) {
		return "", fmt.Errorf("import source path must be relative")
	}
	cleaned := filepath.Clean(relativePath)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("import source path must stay inside source-dir")
	}
	resolved := filepath.Join(directory.root, cleaned)
	if !pathWithin(resolved, directory.root) {
		return "", fmt.Errorf("import source path must stay inside source-dir")
	}
	return resolved, nil
}

// Request is the canonical adoption request used by workflow planning.
type Request struct {
	targets         []targetpkg.Target
	scopes          []targetpkg.Scope
	output          string
	sourceDirectory SourceDirectory
	merge           bool
}

// NewRequest constructs one canonical adoption request.
func NewRequest(
	targets []targetpkg.Target,
	scopes []targetpkg.Scope,
	output string,
	sourceDirectory SourceDirectory,
	merge bool,
) (Request, error) {
	if len(targets) == 0 {
		return Request{}, fmt.Errorf("import request requires at least one target")
	}
	if len(scopes) == 0 {
		return Request{}, fmt.Errorf("import request requires at least one scope")
	}
	if strings.TrimSpace(output) == "" {
		return Request{}, fmt.Errorf("import output is required")
	}
	if !filepath.IsAbs(output) || filepath.Clean(output) != output {
		return Request{}, fmt.Errorf("import output must be an absolute clean path")
	}
	if _, err := NewSourceDirectory(output, sourceDirectory.root); err != nil {
		return Request{}, err
	}

	targetSet := make(map[targetpkg.Target]struct{}, len(targets))
	for _, target := range targets {
		if !SupportsTarget(target) {
			return Request{}, fmt.Errorf("target %q is not supported by import", target)
		}
		if _, duplicate := targetSet[target]; duplicate {
			return Request{}, fmt.Errorf("import request target %q is duplicated", target)
		}
		targetSet[target] = struct{}{}
	}

	scopeSet := make(map[targetpkg.Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		if scope != targetpkg.ScopeProject && scope != targetpkg.ScopeGlobal {
			return Request{}, fmt.Errorf("scope %q is not supported by import", scope)
		}
		if _, duplicate := scopeSet[scope]; duplicate {
			return Request{}, fmt.Errorf("import request scope %q is duplicated", scope)
		}
		scopeSet[scope] = struct{}{}
	}

	return Request{
		targets:         append([]targetpkg.Target(nil), targets...),
		scopes:          append([]targetpkg.Scope(nil), scopes...),
		output:          output,
		sourceDirectory: sourceDirectory,
		merge:           merge,
	}, nil
}

// Validate rejects zero or internally inconsistent requests.
func (request Request) Validate() error {
	_, err := NewRequest(request.targets, request.scopes, request.output, request.sourceDirectory, request.merge)
	return err
}

// Targets returns the ordered target selection.
func (request Request) Targets() []targetpkg.Target {
	return append([]targetpkg.Target(nil), request.targets...)
}

// Scopes returns the ordered scope selection.
func (request Request) Scopes() []targetpkg.Scope {
	return append([]targetpkg.Scope(nil), request.scopes...)
}

// Output returns the selected output manifest path.
func (request Request) Output() string {
	return request.output
}

// SourceDirectory returns the validated imported-source root.
func (request Request) SourceDirectory() SourceDirectory {
	return request.sourceDirectory
}

// Merge reports whether the output manifest must already exist.
func (request Request) Merge() bool {
	return request.merge
}

func pathHasComponent(path string, component string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if part == component {
			return true
		}
	}
	return false
}

func pathWithin(path string, root string) bool {
	relativePath, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if relativePath == "." {
		return true
	}
	if filepath.IsAbs(relativePath) {
		return false
	}
	return relativePath != ".." && !strings.HasPrefix(relativePath, ".."+string(os.PathSeparator))
}
