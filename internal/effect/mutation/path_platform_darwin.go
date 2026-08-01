//go:build darwin

package mutation

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

const darwinPathconfCaseSensitive = 11

type darwinPathObservation struct {
	descriptorPath func(string, bool) (string, error)
	directoryCase  func(string) (pathCaseSemantics, error)
}

func platformCanonicalPath(selection pathSelection, effect PathEffect) (canonicalPath, error) {
	return canonicalDarwinPath(selection, effect, darwinPathObservation{
		descriptorPath: darwinDescriptorPath,
		directoryCase:  darwinDirectoryCaseSemantics,
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
	return canonicalObservedPath(root, components, "darwin-case-v1")
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
	fd, err := unix.Open(path, unix.O_EVTONLY|unix.O_CLOEXEC, 0)
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

func darwinDescriptorPath(path string, noFollow bool) (string, error) {
	flags := unix.O_EVTONLY | unix.O_CLOEXEC | unix.O_NONBLOCK
	if noFollow {
		flags |= unix.O_SYMLINK
	}
	fd, err := unix.Open(path, flags, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)

	buffer := make([]byte, unix.PathMax)
	_, _, errno := unix.Syscall(
		unix.SYS_FCNTL,
		uintptr(fd),
		uintptr(unix.F_GETPATH),
		uintptr(unsafe.Pointer(&buffer[0])),
	)
	runtime.KeepAlive(buffer)
	if errno != 0 {
		return "", errno
	}
	if index := bytes.IndexByte(buffer, 0); index >= 0 {
		buffer = buffer[:index]
	}
	observed := string(buffer)
	if observed == "" || !filepath.IsAbs(observed) || filepath.Clean(observed) != observed {
		return "", fmt.Errorf("F_GETPATH returned invalid path %q", observed)
	}
	return observed, nil
}
