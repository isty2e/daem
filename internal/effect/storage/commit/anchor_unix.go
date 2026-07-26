//go:build darwin || linux

package commit

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

type openedDirectory struct {
	fd       int
	name     string
	path     string
	identity EntryIdentity
	created  bool
}

type anchoredParent struct {
	path        string
	base        string
	directories []openedDirectory
	rootFile    *os.File
	capability  rootedpath.CommitCapability
}

func openCommitParent(
	path string,
	capability rootedpath.CommitCapability,
	createAncestors bool,
) (*anchoredParent, error) {
	if capability == nil {
		return openAnchoredParent(path, createAncestors)
	}
	return openRootedAnchoredParent(path, capability, createAncestors)
}

func openAnchoredParent(path string, createAncestors bool) (*anchoredParent, error) {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}

	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		_ = unix.Close(rootFD)
		return nil, fmt.Errorf("inspect filesystem root: %w", err)
	}
	anchor := &anchoredParent{
		path: path,
		base: filepath.Base(path),
		directories: []openedDirectory{{
			fd:       rootFD,
			path:     "/",
			identity: identityFromStat("/", &rootStat),
		}},
	}

	parentPath := filepath.Dir(path)
	trimmed := strings.TrimPrefix(parentPath, "/")
	if trimmed == "" {
		return anchor, nil
	}
	for component := range strings.SplitSeq(trimmed, "/") {
		if err := anchor.openChildDirectory(component, createAncestors); err != nil {
			return anchor, err
		}
	}
	return anchor, nil
}

func openRootedAnchoredParent(
	path string,
	capability rootedpath.CommitCapability,
	createAncestors bool,
) (*anchoredParent, error) {
	if err := validateRootedCapability(path, capability); err != nil {
		return nil, err
	}
	rootFile, err := capability.OpenRootDirectory()
	if err != nil {
		return nil, err
	}
	var rootStat unix.Stat_t
	if err := unix.Fstat(int(rootFile.Fd()), &rootStat); err != nil {
		_ = rootFile.Close()
		return nil, rootedpath.NewBoundaryFailure(
			rootedpath.FailureRootUnavailable,
			capability.Destination().Root().PhysicalRoot(),
			"inspect rooted commit root",
			err,
		)
	}
	if err := capability.ValidateDirectoryHandle(rootFile.Fd()); err != nil {
		_ = rootFile.Close()
		return nil, err
	}

	destination := capability.Destination()
	anchor := &anchoredParent{
		path:       path,
		base:       filepath.Base(filepath.FromSlash(destination.Relative().Path())),
		rootFile:   rootFile,
		capability: capability,
		directories: []openedDirectory{{
			fd:       int(rootFile.Fd()),
			path:     destination.Root().PhysicalRoot(),
			identity: identityFromStat(destination.Root().PhysicalRoot(), &rootStat),
		}},
	}
	parent := filepath.Dir(filepath.FromSlash(destination.Relative().Path()))
	if parent == "." {
		return anchor, nil
	}
	for component := range strings.SplitSeq(parent, string(filepath.Separator)) {
		if err := anchor.openChildDirectory(component, createAncestors); err != nil {
			return anchor, err
		}
	}
	return anchor, nil
}

func (anchor *anchoredParent) openChildDirectory(name string, create bool) error {
	parent := anchor.directories[len(anchor.directories)-1]
	var before unix.Stat_t
	created := false
	err := unix.Fstatat(parent.fd, name, &before, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) && create {
		if mkdirErr := unix.Mkdirat(parent.fd, name, 0o700); mkdirErr != nil {
			if !errors.Is(mkdirErr, unix.EEXIST) {
				return fmt.Errorf("create ancestor %q: %w", filepath.Join(parent.path, name), mkdirErr)
			}
		} else {
			created = true
		}
		err = unix.Fstatat(parent.fd, name, &before, unix.AT_SYMLINK_NOFOLLOW)
	}
	if err != nil {
		if created {
			anchor.rememberCreated(parent, name, -1, EntryIdentity{})
		}
		return fmt.Errorf("inspect ancestor %q: %w", filepath.Join(parent.path, name), err)
	}
	beforeKind := kindFromStat(&before)
	if anchor.capability != nil && beforeKind == entryKindSymlink {
		kind := rootedpath.FailureAncestorSymlink
		var followed unix.Stat_t
		if followErr := unix.Fstatat(parent.fd, name, &followed, 0); errors.Is(followErr, unix.ENOENT) {
			kind = rootedpath.FailureDanglingAncestorSymlink
		}
		return rootedpath.NewBoundaryFailure(
			kind,
			filepath.Join(parent.path, name),
			"rooted destination ancestor is a symbolic link",
			nil,
		)
	}
	if beforeKind != entryKindDirectory {
		if created {
			anchor.rememberCreated(parent, name, -1, identityFromStat(filepath.Join(parent.path, name), &before))
		}
		if anchor.capability != nil {
			return rootedpath.NewBoundaryFailure(
				rootedpath.FailureAncestorNotDirectory,
				filepath.Join(parent.path, name),
				"rooted destination ancestor is not a directory",
				nil,
			)
		}
		return fmt.Errorf("ancestor %q is not a directory", filepath.Join(parent.path, name))
	}

	fd, err := unix.Openat(parent.fd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if created {
			anchor.rememberCreated(parent, name, -1, identityFromStat(filepath.Join(parent.path, name), &before))
		}
		return fmt.Errorf("open ancestor %q: %w", filepath.Join(parent.path, name), err)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		if created {
			anchor.rememberCreated(parent, name, fd, identityFromStat(filepath.Join(parent.path, name), &before))
		} else {
			_ = unix.Close(fd)
		}
		return fmt.Errorf("inspect opened ancestor %q: %w", filepath.Join(parent.path, name), err)
	}
	beforeIdentity := identityFromStat(filepath.Join(parent.path, name), &before)
	openedIdentity := identityFromStat(filepath.Join(parent.path, name), &opened)
	if !beforeIdentity.sameObject(openedIdentity) {
		_ = unix.Close(fd)
		if created {
			anchor.rememberCreated(parent, name, -1, beforeIdentity)
		}
		if anchor.capability != nil {
			return rootedpath.NewBoundaryFailure(
				rootedpath.FailureAncestorChanged,
				filepath.Join(parent.path, name),
				"rooted destination ancestor changed while opening",
				nil,
			)
		}
		return fmt.Errorf("ancestor %q changed while opening", filepath.Join(parent.path, name))
	}
	anchor.directories = append(anchor.directories, openedDirectory{
		fd:       fd,
		name:     name,
		path:     filepath.Join(parent.path, name),
		identity: openedIdentity,
		created:  created,
	})
	if anchor.capability != nil {
		if err := anchor.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
			return err
		}
	}
	if created {
		if err := unix.Fchmod(fd, 0o700); err != nil {
			return fmt.Errorf("set ancestor mode %q: %w", filepath.Join(parent.path, name), err)
		}
		var final unix.Stat_t
		if err := unix.Fstat(fd, &final); err != nil {
			return fmt.Errorf("verify ancestor metadata %q: %w", filepath.Join(parent.path, name), err)
		}
		if err := validateOwnedStat(filepath.Join(parent.path, name), &final); err != nil {
			return err
		}
		if final.Mode&0o777 != 0o700 {
			return unsupported(fmt.Sprintf("ancestor %q did not retain private mode", filepath.Join(parent.path, name)), nil)
		}
		if err := verifyPreservedMetadata(fd, preservedMetadata{xattrs: make(map[string][]byte)}); err != nil {
			return fmt.Errorf("verify ancestor metadata %q: %w", filepath.Join(parent.path, name), err)
		}
	}
	return nil
}

func (anchor *anchoredParent) rememberCreated(
	parent openedDirectory,
	name string,
	fd int,
	identity EntryIdentity,
) {
	anchor.directories = append(anchor.directories, openedDirectory{
		fd:       fd,
		name:     name,
		path:     filepath.Join(parent.path, name),
		identity: identity,
		created:  true,
	})
}

func (anchor *anchoredParent) parentFD() int {
	return anchor.directories[len(anchor.directories)-1].fd
}

func (anchor *anchoredParent) close() {
	first := 0
	if anchor.rootFile != nil {
		first = 1
	}
	for index := len(anchor.directories) - 1; index >= first; index-- {
		if anchor.directories[index].fd >= 0 {
			_ = unix.Close(anchor.directories[index].fd)
			anchor.directories[index].fd = -1
		}
	}
	if anchor.rootFile != nil {
		_ = anchor.rootFile.Close()
		anchor.rootFile = nil
		if len(anchor.directories) != 0 {
			anchor.directories[0].fd = -1
		}
	}
}

func (anchor *anchoredParent) verifyChain() error {
	if anchor.capability != nil {
		if err := anchor.capability.Validate(); err != nil {
			return err
		}
	}
	for index := 1; index < len(anchor.directories); index++ {
		parent := anchor.directories[index-1]
		current := anchor.directories[index]
		observed, _, err := observeAt(parent.fd, current.name, current.path)
		if err != nil {
			return fmt.Errorf("revalidate ancestor %q: %w", current.path, err)
		}
		if !current.identity.sameObject(observed) {
			if anchor.capability != nil {
				return rootedpath.NewBoundaryFailure(
					rootedpath.FailureAncestorChanged,
					current.path,
					"rooted destination ancestor changed identity",
					nil,
				)
			}
			return fmt.Errorf("ancestor identity changed at %q", current.path)
		}
		if anchor.capability != nil {
			if err := anchor.capability.ValidateDirectoryHandle(uintptr(current.fd)); err != nil {
				return err
			}
		}
	}
	if anchor.capability != nil {
		return anchor.capability.Validate()
	}
	return nil
}
