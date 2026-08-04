package source

import (
	"fmt"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/supply/artifact"
)

const maximumSourceDiagnosticValueBytes = 128

// RootListing is one canonical source-root observation. Child names are safe
// direct source path components; resource-family validity remains downstream.
type RootListing struct {
	sourceID    artifact.SourceID
	resolvedRef artifact.ResolvedRef
	kind        artifact.ArtifactKind
	childNames  []string
}

// NewRootListing constructs a source-correlated, deterministically ordered root listing.
func NewRootListing(
	root Source,
	resolvedRef artifact.ResolvedRef,
	kind artifact.ArtifactKind,
	childNames []string,
) (RootListing, error) {
	sourceID, err := SourceIDFor(root)
	if err != nil {
		return RootListing{}, fmt.Errorf("source root listing: %w", err)
	}
	if err := ValidateResolutionCorrelation(root, sourceID, resolvedRef); err != nil {
		return RootListing{}, fmt.Errorf("source root listing: %w", err)
	}

	listing := RootListing{
		sourceID:    sourceID,
		resolvedRef: resolvedRef,
		kind:        kind,
		childNames:  append([]string(nil), childNames...),
	}
	if err := listing.validate(); err != nil {
		return RootListing{}, err
	}
	sort.Strings(listing.childNames)
	return listing, nil
}

func (listing RootListing) validate() error {
	if err := listing.sourceID.Validate(); err != nil {
		return fmt.Errorf("source root listing: %w", err)
	}
	if err := listing.resolvedRef.Validate(); err != nil {
		return fmt.Errorf("source root listing: %w", err)
	}
	if err := listing.kind.Validate(); err != nil {
		return fmt.Errorf("source root listing: %w", err)
	}
	if listing.kind != artifact.ArtifactKindDirectory && len(listing.childNames) != 0 {
		return fmt.Errorf("source root listing for %s artifact cannot contain child names", listing.kind)
	}

	seen := make(map[string]struct{}, len(listing.childNames))
	for index, childName := range listing.childNames {
		if err := validateChildComponent(childName); err != nil {
			return fmt.Errorf("source root listing child[%d] %s: %w", index, sourceDiagnosticValue(childName), err)
		}
		if _, exists := seen[childName]; exists {
			return fmt.Errorf("source root listing child %s appears more than once", sourceDiagnosticValue(childName))
		}
		seen[childName] = struct{}{}
	}
	return nil
}

// ValidateFor verifies that this listing belongs to root.
func (listing RootListing) ValidateFor(root Source) error {
	if err := listing.validate(); err != nil {
		return err
	}
	if err := ValidateResolutionCorrelation(root, listing.sourceID, listing.resolvedRef); err != nil {
		return fmt.Errorf("source root listing: %w", err)
	}
	return nil
}

// SourceID returns the canonical identity of the listed source root.
func (listing RootListing) SourceID() artifact.SourceID { return listing.sourceID }

// ResolvedRef returns the immutable resolved revision when the source has one.
func (listing RootListing) ResolvedRef() artifact.ResolvedRef { return listing.resolvedRef }

// Kind returns the listed source-root artifact kind.
func (listing RootListing) Kind() artifact.ArtifactKind { return listing.kind }

// IsDirectory reports whether the listed root can contain direct children.
func (listing RootListing) IsDirectory() bool {
	return listing.kind == artifact.ArtifactKindDirectory
}

// ChildNames returns a defensive lexically ordered copy.
func (listing RootListing) ChildNames() []string {
	return append([]string(nil), listing.childNames...)
}

// Child derives one declaration-relative child locator without resolving a ref.
func (source Source) Child(childName string) (Source, error) {
	return source.child(childName, "")
}

// ResolvedChild derives one child locator pinned to resolvedRef where applicable.
func (source Source) ResolvedChild(childName string, resolvedRef artifact.ResolvedRef) (Source, error) {
	sourceID, err := SourceIDFor(source)
	if err != nil {
		return Source{}, err
	}
	if err := ValidateResolutionCorrelation(source, sourceID, resolvedRef); err != nil {
		return Source{}, err
	}
	return source.child(childName, string(resolvedRef))
}

// ValidateChild verifies that child is the declaration-relative or immutably
// resolved locator for childName below this source root.
func (source Source) ValidateChild(childName string, child Source) error {
	declaredChild, err := source.Child(childName)
	if err != nil {
		return err
	}
	declaredID, err := SourceIDFor(declaredChild)
	if err != nil {
		return err
	}
	childID, err := SourceIDFor(child)
	if err != nil {
		return fmt.Errorf("source child %s: %w", sourceDiagnosticValue(childName), err)
	}
	if childID == declaredID {
		return nil
	}

	if source.Kind() == SourceKindGit {
		gitChild, ok := child.Git()
		if ok {
			resolvedChild, resolvedErr := source.ResolvedChild(childName, artifact.ResolvedRef(gitChild.Ref().String()))
			if resolvedErr == nil {
				resolvedID, idErr := SourceIDFor(resolvedChild)
				if idErr == nil && childID == resolvedID {
					return nil
				}
			}
		}
	}

	return fmt.Errorf("source child %s does not belong to source root", sourceDiagnosticValue(childName))
}

func (source Source) child(childName string, resolvedRef string) (Source, error) {
	if err := validateChildComponent(childName); err != nil {
		return Source{}, fmt.Errorf("source child %s: %w", sourceDiagnosticValue(childName), err)
	}

	switch source.Kind() {
	case SourceKindGit:
		gitSource, ok := source.Git()
		if !ok {
			return Source{}, fmt.Errorf("git source data is unavailable")
		}
		ref := gitSource.Ref().String()
		if resolvedRef != "" {
			ref = resolvedRef
		}
		return NewGitSource(
			gitSource.Locator().String(),
			pathpkg.Join(gitSource.RepositoryPath().String(), childName),
			ref,
		)
	case SourceKindLocal:
		localSource, ok := source.Local()
		if !ok {
			return Source{}, fmt.Errorf("local source data is unavailable")
		}
		return NewLocalSource(filepath.Join(localSource.path, childName), localSource.mode)
	case SourceKindS3:
		return Source{}, fmt.Errorf("S3 source children are unsupported; S3 prefix directory sources are unsupported")
	default:
		return Source{}, fmt.Errorf("unsupported source kind %q", source.Kind())
	}
}

func validateChildComponent(value string) error {
	if !utf8.ValidString(value) || value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("must be a non-empty trimmed valid UTF-8 path component")
	}
	if value == "." || value == ".." || strings.ContainsAny(value, "/\\") || pathpkg.Clean(value) != value {
		return fmt.Errorf("must be one relative path component")
	}
	if strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsControl(character) || unicode.In(character, unicode.Cf, unicode.Zl, unicode.Zp)
	}) >= 0 {
		return fmt.Errorf("contains an unsafe control, format, or line-separator character")
	}
	return nil
}

func sourceDiagnosticValue(value string) string {
	if len(value) <= maximumSourceDiagnosticValueBytes {
		return strconv.Quote(value)
	}
	return fmt.Sprintf(
		"%s (bytes=%d)",
		strconv.Quote(value[:maximumSourceDiagnosticValueBytes]),
		len(value),
	)
}
