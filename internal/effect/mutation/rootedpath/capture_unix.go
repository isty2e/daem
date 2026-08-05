//go:build darwin || linux

package rootedpath

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

var errMountIdentityUnsupported = errors.New("native mount identity is unavailable")

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
	platform, err := openPhysicalRootChain(physicalRoot)
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
		// absence is rejected only when a caller requests durable provenance.
		recoveryMount = identityToken{}
	}
	return physicalRoot, platform, object, newMountIdentities(root.mount, recoveryMount), nil
}

func openPhysicalRootChain(physicalRoot string) (capturedRootPlatform, error) {
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
	for _, component := range strings.Split(trimmed, string(filepath.Separator)) {
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
