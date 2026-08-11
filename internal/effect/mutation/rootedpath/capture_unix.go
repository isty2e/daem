//go:build darwin || linux

package rootedpath

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

type capturedDirectory struct {
	fd     int
	name   string
	path   string
	device uint64
	inode  uint64
	mount  identityToken
}

type capturedRootPlatform struct {
	directories []capturedDirectory
}

func captureRootPlatform(
	selectedRoot string,
	selectionMode rootSelectionMode,
	traversal *physicalTraversal,
) (string, capturedRootPlatform, identityToken, mountIdentities, error) {
	if strings.TrimSpace(selectedRoot) == "" {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"selected root is required",
			nil,
		)
	}
	if strings.IndexFunc(selectedRoot, isForbiddenPathRune) >= 0 {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"selected root contains a control character",
			nil,
		)
	}
	if traversal != nil && (!filepath.IsAbs(selectedRoot) || filepath.Clean(selectedRoot) != selectedRoot) {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureInvalidRoot,
			selectedRoot,
			"bounded root must use canonical absolute spelling",
			nil,
		)
	}
	absoluteRoot, err := filepath.Abs(selectedRoot)
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureRootUnavailable,
			selectedRoot,
			"resolve selected root",
			err,
		)
	}
	physicalRoot := filepath.Clean(absoluteRoot)
	switch selectionMode {
	case rootSelectionResolveAlias:
		if traversal != nil {
			physicalRoot, platform, object, mount, missing, resolveErr := resolveDirectoryPathPlatform(
				absoluteRoot,
				false,
				traversal,
			)
			if resolveErr != nil {
				return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, resolveErr
			}
			if len(missing) != 0 {
				_ = closeCapturedRootPlatform(&platform)
				return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
					FailureRootUnavailable,
					absoluteRoot,
					"selected root is unavailable",
					nil,
				)
			}
			return physicalRoot, platform, object, mount, nil
		}
		physicalRoot, err = filepath.EvalSymlinks(absoluteRoot)
		if err != nil {
			return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
				FailureRootUnavailable,
				absoluteRoot,
				"resolve selected root alias",
				err,
			)
		}
		physicalRoot = filepath.Clean(physicalRoot)
	case rootSelectionNoFollow:
	default:
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureInvalidRoot,
			absoluteRoot,
			"root selection mode is invalid",
			nil,
		)
	}
	platform, err := openPhysicalRootChain(physicalRoot, traversal)
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, err
	}
	root := platform.directories[len(platform.directories)-1]
	object, err := nativeObjectToken(root.fd, root.device, root.inode)
	if err != nil {
		_ = closeCapturedRootPlatform(&platform)
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, newFailure(
			FailureRootUnavailable,
			physicalRoot,
			"capture root object identity",
			err,
		)
	}
	recoveryMount, recoveryErr := nativeRecoveryMountToken(root.fd)
	if recoveryErr != nil {
		// Recovery evidence is not part of operation-local root authority. Its
		// failure is rejected only when a caller requests durable provenance.
		return physicalRoot, platform, object, newMountIdentities(
			root.mount,
			unavailableRecoveryMountEvidence(recoveryErr),
		), nil
	}
	return physicalRoot, platform, object, newMountIdentities(
		root.mount,
		availableRecoveryMountEvidence(recoveryMount),
	), nil
}

type pendingNativePathComponent struct {
	name     string
	required bool
}

func resolveDirectoryPathPlatform(
	selectedPath string,
	allowMissing bool,
	traversal *physicalTraversal,
) (string, capturedRootPlatform, identityToken, mountIdentities, []string, error) {
	root, names, err := splitAbsoluteRawPath(selectedPath)
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, err
	}
	rootFD, err := unix.Open(root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, newFailure(
			FailureRootUnavailable,
			selectedPath,
			"open filesystem root",
			err,
		)
	}
	rootDirectory, err := captureDirectory(rootFD, "", root)
	if err != nil {
		_ = unix.Close(rootFD)
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, err
	}
	platform := capturedRootPlatform{directories: []capturedDirectory{rootDirectory}}
	fail := func(err error) (string, capturedRootPlatform, identityToken, mountIdentities, []string, error) {
		_ = closeCapturedRootPlatform(&platform)
		return "", capturedRootPlatform{}, identityToken{}, mountIdentities{}, nil, err
	}

	pending := nativePathComponents(names, false)
	symlinks := 0
	for len(pending) != 0 {
		component := pending[0]
		pending = pending[1:]
		switch component.name {
		case "":
			continue
		case ".":
			if err := traversal.visitComponent(); err != nil {
				return fail(err)
			}
			continue
		case "..":
			if err := traversal.visitComponent(); err != nil {
				return fail(err)
			}
			if len(platform.directories) > 1 {
				last := len(platform.directories) - 1
				if err := unix.Close(platform.directories[last].fd); err != nil {
					return fail(newFailure(FailureRootUnavailable, selectedPath, "close parent-traversed directory", err))
				}
				platform.directories = platform.directories[:last]
			}
			continue
		}

		if err := traversal.visitComponent(); err != nil {
			return fail(err)
		}
		parent := platform.directories[len(platform.directories)-1]
		candidatePath := filepath.Join(parent.path, component.name)
		var stat unix.Stat_t
		inspectErr := unix.Fstatat(parent.fd, component.name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(inspectErr, unix.ENOENT) {
			if component.required || !allowMissing {
				return fail(newFailure(
					FailureRootUnavailable,
					candidatePath,
					"selected root alias target is unavailable",
					inspectErr,
				))
			}
			missing := []string{component.name}
			for _, remainder := range pending {
				if remainder.name != "" && remainder.name != "." {
					missing = append(missing, remainder.name)
				}
			}
			object, mount, missing, identityErr := resolvedDirectoryIdentity(platform, missing)
			if identityErr != nil {
				return fail(identityErr)
			}
			return resolvedDirectoryPath(platform), platform, object, mount, missing, nil
		}
		if inspectErr != nil {
			return fail(newFailure(FailureRootUnavailable, candidatePath, "inspect destination ancestor", inspectErr))
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFLNK {
			symlinks++
			if symlinks > maximumPathSymlinkExpansions {
				return fail(newFailure(
					FailureRootUnavailable,
					candidatePath,
					"resolve selected root alias",
					fmt.Errorf("too many symbolic links"),
				))
			}
			target, readErr := readlinkAt(parent.fd, component.name)
			if readErr != nil {
				return fail(newFailure(FailureRootUnavailable, candidatePath, "read selected root alias", readErr))
			}
			absoluteTarget, targetNames := rawPathComponents(target)
			if absoluteTarget {
				for len(platform.directories) > 1 {
					last := len(platform.directories) - 1
					if err := unix.Close(platform.directories[last].fd); err != nil {
						return fail(newFailure(FailureRootUnavailable, candidatePath, "reset absolute alias target", err))
					}
					platform.directories = platform.directories[:last]
				}
			}
			pending = append(nativePathComponents(targetNames, true), pending...)
			continue
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
			return fail(newFailure(
				FailureRootUnavailable,
				candidatePath,
				"destination ancestor is not a directory",
				nil,
			))
		}
		resolvedDepth := len(platform.directories)
		if err := traversal.validateResolvedDepth(resolvedDepth); err != nil {
			return fail(err)
		}
		directory, openErr := openCapturedChild(parent, component.name, candidatePath)
		if openErr != nil {
			return fail(openErr)
		}
		platform.directories = append(platform.directories, directory)
	}

	physicalRoot := resolvedDirectoryPath(platform)
	object, mount, _, identityErr := resolvedDirectoryIdentity(platform, nil)
	if identityErr != nil {
		return fail(identityErr)
	}
	return physicalRoot, platform, object, mount, nil, nil
}

func resolvedDirectoryIdentity(
	platform capturedRootPlatform,
	missing []string,
) (identityToken, mountIdentities, []string, error) {
	root := platform.directories[len(platform.directories)-1]
	object, err := nativeObjectToken(root.fd, root.device, root.inode)
	if err != nil {
		return identityToken{}, mountIdentities{}, nil, newFailure(
			FailureRootUnavailable,
			root.path,
			"capture root object identity",
			err,
		)
	}
	recoveryMount, recoveryErr := nativeRecoveryMountToken(root.fd)
	if recoveryErr != nil {
		return object, newMountIdentities(
			root.mount,
			unavailableRecoveryMountEvidence(recoveryErr),
		), missing, nil
	}
	return object, newMountIdentities(
		root.mount,
		availableRecoveryMountEvidence(recoveryMount),
	), missing, nil
}

func resolvedDirectoryPath(platform capturedRootPlatform) string {
	return platform.directories[len(platform.directories)-1].path
}

func splitAbsoluteRawPath(value string) (string, []string, error) {
	if !filepath.IsAbs(value) {
		return "", nil, newFailure(FailureInvalidRoot, value, "path must be absolute", nil)
	}
	absolute, names := rawPathComponents(value)
	if !absolute {
		return "", nil, newFailure(FailureInvalidRoot, value, "path must be absolute", nil)
	}
	return string(filepath.Separator), names, nil
}

func rawPathComponents(value string) (bool, []string) {
	absolute := strings.HasPrefix(value, string(filepath.Separator))
	names := strings.Split(value, string(filepath.Separator))
	for len(names) != 0 && names[0] == "" {
		names = names[1:]
	}
	return absolute, names
}

func nativePathComponents(names []string, required bool) []pendingNativePathComponent {
	components := make([]pendingNativePathComponent, 0, len(names))
	for _, name := range names {
		components = append(components, pendingNativePathComponent{name: name, required: required})
	}
	return components
}

func readlinkAt(parentFD int, name string) (string, error) {
	for size := 256; size <= 1<<20; size *= 2 {
		buffer := make([]byte, size)
		count, err := unix.Readlinkat(parentFD, name, buffer)
		if err != nil {
			return "", err
		}
		if count < len(buffer) {
			return string(buffer[:count]), nil
		}
	}
	return "", fmt.Errorf("symbolic link target exceeds 1 MiB")
}

func openPhysicalRootChain(
	physicalRoot string,
	traversal *physicalTraversal,
) (capturedRootPlatform, error) {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return capturedRootPlatform{}, newFailure(FailureRootUnavailable, physicalRoot, "open filesystem root", err)
	}
	rootDirectory, err := captureDirectory(rootFD, "", "/")
	if err != nil {
		_ = unix.Close(rootFD)
		return capturedRootPlatform{}, err
	}
	platform := capturedRootPlatform{directories: []capturedDirectory{rootDirectory}}

	trimmed := strings.TrimPrefix(physicalRoot, string(filepath.Separator))
	if trimmed == "" {
		_ = closeCapturedRootPlatform(&platform)
		return capturedRootPlatform{}, newFailure(FailureInvalidRoot, physicalRoot, "filesystem root cannot be root authority", nil)
	}
	for index, component := range strings.Split(trimmed, string(filepath.Separator)) {
		if err := traversal.visitComponent(); err != nil {
			_ = closeCapturedRootPlatform(&platform)
			return capturedRootPlatform{}, err
		}
		if err := traversal.validateResolvedDepth(index + 1); err != nil {
			_ = closeCapturedRootPlatform(&platform)
			return capturedRootPlatform{}, err
		}
		parent := platform.directories[len(platform.directories)-1]
		path := filepath.Join(parent.path, component)
		directory, err := openCapturedChild(parent, component, path)
		if err != nil {
			_ = closeCapturedRootPlatform(&platform)
			return capturedRootPlatform{}, err
		}
		platform.directories = append(platform.directories, directory)
	}
	return platform, nil
}

func openCapturedChild(parent capturedDirectory, name string, path string) (capturedDirectory, error) {
	var before unix.Stat_t
	if err := unix.Fstatat(parent.fd, name, &before, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return capturedDirectory{}, newFailure(FailureRootUnavailable, path, "inspect physical root component", err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return capturedDirectory{}, newFailure(FailureRootReplaced, path, "physical root component is not a directory", nil)
	}
	fd, err := unix.Openat(parent.fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return capturedDirectory{}, newFailure(FailureRootUnavailable, path, "open physical root component", err)
	}
	directory, err := captureDirectory(fd, name, path)
	if err != nil {
		_ = unix.Close(fd)
		return capturedDirectory{}, err
	}
	if directory.device != uint64(before.Dev) || directory.inode != uint64(before.Ino) {
		_ = unix.Close(fd)
		return capturedDirectory{}, newFailure(FailureRootReplaced, path, "physical root component changed while opening", nil)
	}
	return directory, nil
}

func capturedRootChildExistsNoFollow(platform *capturedRootPlatform, name string) (bool, error) {
	if platform == nil || len(platform.directories) == 0 {
		return false, newFailure(FailureRootUnavailable, name, "captured root is unavailable", nil)
	}
	root := platform.directories[len(platform.directories)-1]
	var stat unix.Stat_t
	err := unix.Fstatat(root.fd, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ENOENT):
		return false, nil
	default:
		return false, newFailure(FailureRootUnavailable, filepath.Join(root.path, name), "observe retained-root child", err)
	}
}

func capturedRootValidationPathComponents(platform *capturedRootPlatform) (int, error) {
	if platform == nil || len(platform.directories) < 2 {
		return 0, newFailure(FailureRootUnavailable, "", "captured root witness is not initialized", nil)
	}
	return len(platform.directories) - 1, nil
}

func captureDirectory(fd int, name string, path string) (capturedDirectory, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return capturedDirectory{}, newFailure(FailureRootUnavailable, path, "inspect captured directory", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return capturedDirectory{}, newFailure(FailureRootReplaced, path, "captured root component is not a directory", nil)
	}
	mount, err := nativeMountToken(fd)
	if err != nil {
		kind := FailureRootUnavailable
		if errors.Is(err, errMountIdentityUnsupported) {
			kind = FailureUnsupportedPlatform
		}
		return capturedDirectory{}, newFailure(kind, path, "capture directory mount identity", err)
	}
	return capturedDirectory{
		fd:     fd,
		name:   name,
		path:   path,
		device: uint64(stat.Dev),
		inode:  uint64(stat.Ino),
		mount:  mount,
	}, nil
}
