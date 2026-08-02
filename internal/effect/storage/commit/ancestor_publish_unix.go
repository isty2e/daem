//go:build darwin || linux

package commit

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func (anchor *anchoredParent) createAndPublishChildDirectory(
	parent openedDirectory,
	name string,
) error {
	path := filepath.Join(parent.path, name)
	stageName, stageFD, identity, err := anchor.createStagedChildDirectory(parent, path)
	if err != nil {
		return fmt.Errorf("create staged ancestor for %q: %w", path, err)
	}
	stagePath := filepath.Join(parent.path, stageName)
	published := false
	defer func() {
		if !published {
			_ = unix.Close(stageFD)
		}
	}()

	if anchor.ancestorPublicationHooks.before != nil {
		anchor.ancestorPublicationHooks.before(path)
	}
	err = renameNoReplace(parent.fd, stageName, parent.fd, name)
	if errors.Is(err, unix.EEXIST) {
		if cleanupErr := cleanupStagedDirectory(parent, stageName, stagePath, identity); cleanupErr != nil {
			anchor.rememberUnpublishedResidue(stagePath)
			return errors.Join(err, cleanupErr)
		}
		var concurrent unix.Stat_t
		if inspectErr := unix.Fstatat(parent.fd, name, &concurrent, unix.AT_SYMLINK_NOFOLLOW); inspectErr != nil {
			return fmt.Errorf("inspect concurrently created ancestor %q: %w", path, inspectErr)
		}
		return anchor.openObservedChildDirectory(parent, name, &concurrent)
	}
	if err != nil {
		cleanupErr := cleanupStagedDirectory(parent, stageName, stagePath, identity)
		if cleanupErr != nil {
			anchor.rememberUnpublishedResidue(stagePath)
		}
		return errors.Join(fmt.Errorf("publish ancestor %q: %w", path, err), cleanupErr)
	}
	published = true

	anchor.directories = append(anchor.directories, openedDirectory{
		fd:       stageFD,
		name:     name,
		path:     path,
		identity: identity,
		created:  true,
	})
	if anchor.ancestorPublicationHooks.after != nil {
		anchor.ancestorPublicationHooks.after(path)
	}
	observed, _, observeErr := observeAt(parent.fd, name, path)
	if observeErr != nil || !identity.sameObject(observed) {
		return fmt.Errorf(
			"published ancestor %q changed identity: %w",
			path,
			errors.Join(observeErr, errAncestorIdentityChanged),
		)
	}
	return nil
}

var errAncestorIdentityChanged = errors.New("published ancestor does not identify the staged object")

func (anchor *anchoredParent) createStagedChildDirectory(
	parent openedDirectory,
	path string,
) (string, int, EntryIdentity, error) {
	for range 64 {
		stageName, err := randomSiblingName(temporaryPrefix)
		if err != nil {
			return "", -1, EntryIdentity{}, err
		}
		if err := unix.Mkdirat(parent.fd, stageName, 0o700); errors.Is(err, unix.EEXIST) {
			continue
		} else if err != nil {
			return "", -1, EntryIdentity{}, err
		}
		stagePath := filepath.Join(parent.path, stageName)
		fd, err := unix.Openat(
			parent.fd,
			stageName,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0,
		)
		if err != nil {
			anchor.rememberUnpublishedResidue(stagePath)
			return stageName, -1, EntryIdentity{}, err
		}
		var stat unix.Stat_t
		if err := unix.Fstat(fd, &stat); err != nil {
			_ = unix.Close(fd)
			anchor.rememberUnpublishedResidue(stagePath)
			return stageName, -1, EntryIdentity{}, err
		}
		identity := identityFromStat(path, &stat)
		observed, observedStat, err := observeAt(parent.fd, stageName, stagePath)
		if err != nil || !identity.sameObject(observed) {
			_ = unix.Close(fd)
			anchor.rememberUnpublishedResidue(stagePath)
			return stageName, -1, EntryIdentity{}, errors.Join(err, errAncestorIdentityChanged)
		}
		if err := validateOwnedStat(stagePath, &observedStat); err != nil {
			return "", -1, EntryIdentity{}, anchor.failStagedDirectoryCreation(
				parent,
				stageName,
				stagePath,
				fd,
				identity,
				err,
			)
		}
		if anchor.capability != nil {
			if err := anchor.capability.ValidateDirectoryHandle(uintptr(fd)); err != nil {
				return "", -1, EntryIdentity{}, anchor.failStagedDirectoryCreation(
					parent,
					stageName,
					stagePath,
					fd,
					identity,
					err,
				)
			}
		}
		if err := verifyCreatedDirectoryMetadata(fd, stagePath); err != nil {
			return "", -1, EntryIdentity{}, anchor.failStagedDirectoryCreation(
				parent,
				stageName,
				stagePath,
				fd,
				identity,
				err,
			)
		}
		return stageName, fd, identity, nil
	}
	return "", -1, EntryIdentity{}, fmt.Errorf("could not allocate a collision-free ancestor stage")
}

func (anchor *anchoredParent) failStagedDirectoryCreation(
	parent openedDirectory,
	name string,
	path string,
	fd int,
	expected EntryIdentity,
	cause error,
) error {
	cleanupErr := cleanupStagedDirectory(parent, name, path, expected)
	if cleanupErr != nil {
		anchor.rememberUnpublishedResidue(path)
	}
	closeErr := unix.Close(fd)
	return errors.Join(cause, cleanupErr, closeErr)
}

func (anchor *anchoredParent) rememberUnpublishedResidue(path string) {
	anchor.unpublishedResidue = append(anchor.unpublishedResidue, path)
}

func verifyCreatedDirectoryMetadata(fd int, path string) error {
	if err := unix.Fchmod(fd, 0o700); err != nil {
		return fmt.Errorf("set ancestor mode %q: %w", path, err)
	}
	var final unix.Stat_t
	if err := unix.Fstat(fd, &final); err != nil {
		return fmt.Errorf("verify ancestor metadata %q: %w", path, err)
	}
	if err := validateOwnedStat(path, &final); err != nil {
		return err
	}
	if final.Mode&0o777 != 0o700 {
		return unsupported(fmt.Sprintf("ancestor %q did not retain private mode", path), nil)
	}
	if err := verifyPreservedMetadata(fd, preservedMetadata{xattrs: make(map[string][]byte)}); err != nil {
		return fmt.Errorf("verify ancestor metadata %q: %w", path, err)
	}
	return nil
}

func cleanupStagedDirectory(
	parent openedDirectory,
	name string,
	path string,
	expected EntryIdentity,
) error {
	observed, _, err := observeAt(parent.fd, name, path)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect staged ancestor %q during cleanup: %w", path, err)
	}
	if !expected.sameObject(observed) {
		return fmt.Errorf("staged ancestor identity changed at %q", path)
	}
	if err := unix.Unlinkat(parent.fd, name, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove staged ancestor %q: %w", path, err)
	}
	if err := syncDirectory(parent.fd); err != nil {
		return fmt.Errorf("sync staged ancestor parent %q: %w", parent.path, err)
	}
	return nil
}
