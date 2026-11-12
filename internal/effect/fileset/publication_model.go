package fileset

import (
	"cmp"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// PublicationImage identifies an absent or exact regular-file image. It does
// not retain content or confer authority to read, write or remove an entry.
type PublicationImage struct {
	exists      bool
	size        int64
	hash        string
	mode        fs.FileMode
	initialized bool
}

func AbsentPublicationImage() PublicationImage {
	return PublicationImage{initialized: true}
}

func NewPublicationImage(content []byte, mode fs.FileMode) (PublicationImage, error) {
	if err := validateTargetContentLength(int64(len(content))); err != nil {
		return PublicationImage{}, err
	}
	return publicationImageFromFacts(true, int64(len(content)), hashBytes(content), mode)
}

func (image PublicationImage) Exists() bool      { return image.exists }
func (image PublicationImage) Size() int64       { return image.size }
func (image PublicationImage) Hash() string      { return image.hash }
func (image PublicationImage) Mode() fs.FileMode { return image.mode }

func (image PublicationImage) Equal(other PublicationImage) bool { return image == other }

func (image PublicationImage) Validate() error {
	if !image.initialized {
		return fmt.Errorf("publication image is not initialized")
	}
	if !image.exists {
		if image.size != 0 || image.hash != "" || image.mode != 0 {
			return fmt.Errorf("absent publication image carries file facts")
		}
		return nil
	}
	if err := validateTargetContentLength(image.size); err != nil {
		return err
	}
	if !isPublicationHash(image.hash) {
		return fmt.Errorf("publication image requires a canonical SHA-256 digest")
	}
	if image.size == 0 && image.hash != hashBytes(nil) {
		return fmt.Errorf("empty publication image has an inconsistent digest")
	}
	if image.mode&^fs.ModePerm != 0 {
		return fmt.Errorf("publication image mode must contain only permission bits")
	}
	return nil
}

func publicationImageFromFacts(exists bool, size int64, digest string, mode fs.FileMode) (PublicationImage, error) {
	image := PublicationImage{exists: exists, size: size, hash: digest, mode: mode, initialized: true}
	if err := image.Validate(); err != nil {
		return PublicationImage{}, err
	}
	return image, nil
}

func isPublicationHash(value string) bool {
	return isHash(value) && value == strings.ToLower(value)
}

// PublicationTarget records an absolute clean namespace spelling and its images.
// Callers must independently qualify and lease it; spelling is not canonicalization.
type PublicationTarget struct {
	path   string
	before PublicationImage
	after  PublicationImage
}

func NewPublicationTarget(path string, before, after PublicationImage) (PublicationTarget, error) {
	target := PublicationTarget{path: path, before: before, after: after}
	if err := target.Validate(); err != nil {
		return PublicationTarget{}, err
	}
	return target, nil
}

func (target PublicationTarget) Path() string             { return target.path }
func (target PublicationTarget) Before() PublicationImage { return target.before }
func (target PublicationTarget) After() PublicationImage  { return target.after }
func (target PublicationTarget) Writes() bool             { return !target.before.Equal(target.after) }

func (target PublicationTarget) Validate() error {
	if !utf8.ValidString(target.path) || strings.ContainsRune(target.path, 0) ||
		target.path == "" || !filepath.IsAbs(target.path) || filepath.Clean(target.path) != target.path {
		return fmt.Errorf("publication target path must be absolute and clean")
	}
	if err := target.before.Validate(); err != nil {
		return fmt.Errorf("publication Before image: %w", err)
	}
	if err := target.after.Validate(); err != nil {
		return fmt.Errorf("publication After image: %w", err)
	}
	if target.before.exists && !target.after.exists {
		return fmt.Errorf("publication does not admit deletion targets")
	}
	return nil
}

// PublicationIntent owns the mechanical target facts and an opaque semantic
// selection. Record validation neither reserves physical work nor grants access.
// Targets have non-nested path spellings and are stored in lexical path order,
// independently of their input order.
type PublicationIntent struct {
	selection []byte
	targets   []PublicationTarget
}

func NewPublicationIntent(selection []byte, targets []PublicationTarget) (PublicationIntent, error) {
	if len(selection) == 0 {
		return PublicationIntent{}, fmt.Errorf("publication semantic selection is required")
	}
	if int64(len(selection)) > maximumTargetBytes {
		return PublicationIntent{}, fmt.Errorf("publication semantic selection exceeds retained image limit")
	}
	if len(targets) == 0 {
		return PublicationIntent{}, fmt.Errorf("publication targets are required")
	}
	if int64(len(targets)) > maximumTargetBytes {
		return PublicationIntent{}, fmt.Errorf("publication target records cannot fit retained intent byte limit")
	}
	for index, target := range targets {
		if err := target.Validate(); err != nil {
			return PublicationIntent{}, fmt.Errorf("publication target[%d]: %w", index, err)
		}
	}

	canonical := slices.Clone(targets)
	slices.SortFunc(canonical, func(left, right PublicationTarget) int { return cmp.Compare(left.path, right.path) })
	for index := 1; index < len(canonical); index++ {
		if canonical[index-1].path == canonical[index].path {
			return PublicationIntent{}, fmt.Errorf("publication target %q appears more than once", canonical[index].path)
		}
	}
	for _, target := range canonical {
		prefix := strings.TrimSuffix(target.path, string(filepath.Separator)) + string(filepath.Separator)
		index, _ := slices.BinarySearchFunc(canonical, prefix, func(target PublicationTarget, path string) int {
			return cmp.Compare(target.path, path)
		})
		if index < len(canonical) && canonical[index].path == target.path {
			index++
		}
		if index < len(canonical) && strings.HasPrefix(canonical[index].path, prefix) {
			return PublicationIntent{}, fmt.Errorf("publication targets contain nested namespaces")
		}
	}
	var beforeBytes int64
	for _, target := range canonical {
		if target.before.size > maximumStagedBeforeImageBytes-beforeBytes {
			return PublicationIntent{}, fmt.Errorf("publication Before images exceed captured byte limit")
		}
		beforeBytes += target.before.size
	}

	intent := PublicationIntent{selection: slices.Clone(selection), targets: canonical}
	if _, err := encodePublicationIntent(intent); err != nil {
		return PublicationIntent{}, err
	}
	return intent, nil
}

func (intent PublicationIntent) Selection() []byte { return slices.Clone(intent.selection) }

func (intent PublicationIntent) Targets() []PublicationTarget { return slices.Clone(intent.targets) }

func (intent PublicationIntent) Validate() error {
	if len(intent.selection) == 0 || len(intent.targets) == 0 {
		return fmt.Errorf("publication intent is not initialized")
	}
	return nil
}

func (intent PublicationIntent) HasChanges() bool {
	for _, target := range intent.targets {
		if target.Writes() {
			return true
		}
	}
	return false
}
