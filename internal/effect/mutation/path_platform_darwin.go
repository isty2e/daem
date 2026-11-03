//go:build darwin

package mutation

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unsafe"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

const (
	darwinPathconfCaseSensitive = 11
	darwinPathOpenSearch        = 0x40000000 | unix.O_DIRECTORY
)

type darwinPathObservation struct {
	descriptorPath func(string, bool) (string, error)
	directoryCase  func(string) (pathCaseSemantics, error)
}

func platformCanonicalPath(selection pathSelection, effect PathEffect) (canonicalPath, error) {
	return canonicalDarwinPath(selection, effect, darwinPathObservation{
		descriptorPath: darwinStoredPath,
		directoryCase:  darwinDirectoryCaseSemantics,
	})
}

func newPlatformPathObservation() func(pathSelection, PathEffect) (canonicalPath, error) {
	observation := darwinPathObservation{
		descriptorPath: darwinStoredPath,
		directoryCase:  darwinDirectoryCaseSemantics,
	}.shared()
	return func(selection pathSelection, effect PathEffect) (canonicalPath, error) {
		return canonicalDarwinPath(selection, effect, observation)
	}
}

func (observation darwinPathObservation) shared() darwinPathObservation {
	type descriptorRequest struct {
		path     string
		noFollow bool
	}
	descriptors := make(map[descriptorRequest]string)
	cases := make(map[string]pathCaseSemantics)
	return darwinPathObservation{
		descriptorPath: func(path string, noFollow bool) (string, error) {
			request := descriptorRequest{path: path, noFollow: noFollow}
			if stored, ok := descriptors[request]; ok {
				return stored, nil
			}
			stored, err := observation.descriptorPath(path, noFollow)
			if err == nil {
				descriptors[request] = stored
			}
			return stored, err
		},
		directoryCase: func(path string) (pathCaseSemantics, error) {
			if mode, ok := cases[path]; ok {
				return mode, nil
			}
			mode, err := observation.directoryCase(path)
			if err == nil {
				cases[path] = mode
			}
			return mode, err
		},
	}
}

func platformCanonicalPathBounded(
	selection pathSelection,
	effect PathEffect,
	maximumPhysicalDepth int,
	budget rootedpath.PhysicalTraversalBudget,
) (canonicalPath, error) {
	return canonicalDarwinPath(selection, effect, darwinPathObservation{
		descriptorPath: func(path string, noFollow bool) (string, error) {
			if err := admitPlatformPathTraversal(path, maximumPhysicalDepth, budget); err != nil {
				return "", err
			}
			return darwinStoredPath(path, noFollow)
		},
		directoryCase: func(path string) (pathCaseSemantics, error) {
			if err := admitPlatformPathTraversal(path, maximumPhysicalDepth, budget); err != nil {
				return 0, err
			}
			return darwinDirectoryCaseSemantics(path)
		},
	})
}

func canonicalDarwinPath(
	selection pathSelection,
	effect PathEffect,
	observation darwinPathObservation,
) (canonicalPath, error) {
	if observation.descriptorPath == nil || observation.directoryCase == nil {
		return canonicalPath{}, fmt.Errorf("Darwin path observation is incomplete")
	}
	actualAnchor, err := observation.descriptorPath(selection.anchorPath, false)
	if err != nil {
		return canonicalPath{}, fmt.Errorf("observe stored path spelling for %q: %w", selection.anchorPath, err)
	}
	actualExisting := actualAnchor
	missing := append([]string(nil), selection.missingComponents...)
	if effect == PathEffectDirectoryEntry && selection.finalEntryMayExist && len(missing) == 1 {
		candidate := filepath.Join(actualAnchor, missing[0])
		actualFinal, finalErr := observation.descriptorPath(candidate, true)
		if finalErr == nil {
			actualExisting = actualFinal
			missing = nil
		} else if !os.IsNotExist(finalErr) {
			return canonicalPath{}, fmt.Errorf("observe stored directory entry spelling for %q: %w", candidate, finalErr)
		}
	}

	root, existingNames, err := darwinPathComponents(actualExisting)
	if err != nil {
		return canonicalPath{}, err
	}
	components := make([]observedPathComponent, 0, len(existingNames)+len(missing))
	parent := root
	for _, name := range existingNames {
		caseMode, err := observation.directoryCase(parent)
		if err != nil {
			return canonicalPath{}, fmt.Errorf("observe case semantics for directory %q: %w", parent, err)
		}
		components = append(components, observedPathComponent{spelling: name, caseMode: caseMode})
		parent = filepath.Join(parent, name)
	}
	if len(missing) != 0 {
		caseMode, err := observation.directoryCase(actualExisting)
		if err != nil {
			return canonicalPath{}, fmt.Errorf("observe case semantics for existing ancestor %q: %w", actualExisting, err)
		}
		for _, name := range missing {
			components = append(components, observedPathComponent{spelling: name, caseMode: caseMode})
		}
	}
	identity, err := canonicalObservedPath(root, components, "darwin-case-v1")
	if err != nil {
		return canonicalPath{}, err
	}

	normalizationSensitiveMissing := containsNonASCIIPathComponent(missing)
	normalizationSensitiveExistingEntry := effect == PathEffectDirectoryEntry &&
		len(missing) == 0 && len(existingNames) != 0 && containsNonASCII(existingNames[len(existingNames)-1])
	if !normalizationSensitiveMissing && !normalizationSensitiveExistingEntry {
		return identity, nil
	}

	namespaceComponentCount := len(existingNames)
	if normalizationSensitiveExistingEntry {
		namespaceComponentCount--
	}
	namespace, err := canonicalObservedPath(root, components[:namespaceComponentCount], "darwin-case-v1")
	if err != nil {
		return canonicalPath{}, fmt.Errorf("canonicalize normalization namespace: %w", err)
	}
	namespaceLease, err := newNamespaceLeaseIntent(namespace)
	if err != nil {
		return canonicalPath{}, err
	}
	identity.namespaceLease = namespaceLease
	if normalizationSensitiveMissing {
		provisional, err := newProvisionalPathIntent(identity, namespace)
		if err != nil {
			return canonicalPath{}, err
		}
		identity.provisional = provisional
	}
	return identity, nil
}

func containsNonASCIIPathComponent(components []string) bool {
	return slices.ContainsFunc(components, containsNonASCII)
}

func containsNonASCII(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] >= 0x80 {
			return true
		}
	}
	return false
}

func darwinPathComponents(path string) (string, []string, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return "", nil, fmt.Errorf("observed Darwin path %q is not absolute", path)
	}
	volume := filepath.VolumeName(clean)
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(clean, root)
	if relative == clean {
		return "", nil, fmt.Errorf("observed Darwin path %q has no rooted prefix", path)
	}
	if relative == "" {
		return root, nil, nil
	}
	return root, strings.Split(relative, string(filepath.Separator)), nil
}

func darwinDirectoryCaseSemantics(path string) (pathCaseSemantics, error) {
	fd, err := unix.Open(path, darwinPathOpenSearch|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)
	value, err := unix.Fpathconf(fd, darwinPathconfCaseSensitive)
	if err != nil {
		return 0, err
	}
	switch value {
	case 0:
		return pathCaseInsensitive, nil
	case 1:
		return pathCaseSensitive, nil
	default:
		return 0, fmt.Errorf("_PC_CASE_SENSITIVE returned unsupported value %d", value)
	}
}

// Namespace identity needs stored spelling, not permission to read the payload.
func darwinStoredPath(path string, noFollow bool) (string, error) {
	name, err := unix.BytePtrFromString(path)
	if err != nil {
		return "", err
	}
	attributes := unix.Attrlist{Bitmapcount: unix.ATTR_BIT_MAP_COUNT, Commonattr: unix.ATTR_CMN_FULLPATH}
	options := uintptr(0)
	if noFollow {
		options = unix.FSOPT_NOFOLLOW
	}
	const headerBytes = 12
	buffer := make([]byte, unix.PathMax+headerBytes)
	_, _, errno := unix.Syscall6(
		unix.SYS_GETATTRLIST,
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)), options, 0,
	)
	runtime.KeepAlive(name)
	runtime.KeepAlive(attributes)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return "", errno
	}
	total := int64(binary.LittleEndian.Uint32(buffer[:4]))
	start := 4 + int64(int32(binary.LittleEndian.Uint32(buffer[4:8])))
	length := int64(binary.LittleEndian.Uint32(buffer[8:12]))
	if total < headerBytes || total > int64(len(buffer)) || start < headerBytes || length <= 1 || start+length > total {
		return "", fmt.Errorf("getattrlist returned an invalid full-path attribute")
	}
	value := buffer[start : start+length]
	if bytes.IndexByte(value, 0) != len(value)-1 {
		return "", fmt.Errorf("getattrlist returned an unterminated full path")
	}
	observed := string(value[:len(value)-1])
	if observed == "" || !filepath.IsAbs(observed) || filepath.Clean(observed) != observed {
		return "", fmt.Errorf("getattrlist returned invalid path %q", observed)
	}
	return observed, nil
}
