//go:build darwin || linux

package access

import (
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"golang.org/x/sys/unix"
)

func openNativeRoot(root string, expectedKind artifact.ArtifactKind) (*nativeRoot, error) {
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open artifact filesystem root: %w", err)
	}
	if root == "/" {
		entry, err := openedNativeEntry(rootFD)
		if err != nil {
			_ = unix.Close(rootFD)
			return nil, err
		}
		handle := &nativeRoot{parentFD: -1, entry: entry}
		if expectedKind != "" {
			if err := handle.requireKind(expectedKind); err != nil {
				_ = handle.close()
				return nil, err
			}
		}
		return handle, nil
	}

	components := strings.Split(strings.TrimPrefix(root, "/"), "/")
	parentFD := rootFD
	for index, component := range components {
		last := index == len(components)-1
		entry, err := openNativePathComponent(parentFD, component, last)
		if err != nil {
			_ = unix.Close(parentFD)
			return nil, fmt.Errorf("open artifact root component %q: %w", component, err)
		}
		if last {
			handle := &nativeRoot{
				name:     component,
				parentFD: parentFD,
				entry:    entry,
			}
			if expectedKind != "" {
				if err := handle.requireKind(expectedKind); err != nil {
					_ = handle.close()
					return nil, err
				}
			}
			return handle, nil
		}
		if entry.kind != nativeKindDirectory {
			return nil, errors.Join(
				fmt.Errorf("artifact root ancestor %q is not a directory", component),
				unix.Close(entry.fd),
				unix.Close(parentFD),
			)
		}
		if closeErr := unix.Close(parentFD); closeErr != nil {
			return nil, errors.Join(closeErr, unix.Close(entry.fd))
		}
		parentFD = entry.fd
	}
	_ = unix.Close(parentFD)
	return nil, fmt.Errorf("artifact access root %q has no path components", root)
}

func (root *nativeRoot) requireKind(expected artifact.ArtifactKind) error {
	want := nativeKindFile
	if expected == artifact.ArtifactKindDirectory {
		want = nativeKindDirectory
	}
	if root.entry.kind != want {
		return fmt.Errorf("artifact access root kind %q does not match expected kind %q", publicEntryKind(root.entry.kind), expected)
	}
	return nil
}

type relativeNativeEntry struct {
	parentFD    int
	parentOwned bool
	name        string
	entry       nativeEntry
}

func (root *nativeRoot) openRelative(relativePath string) (*relativeNativeEntry, error) {
	if relativePath == "." {
		return nil, nil
	}
	if root.entry.kind != nativeKindDirectory {
		return nil, fmt.Errorf("artifact file root has no child path %q", relativePath)
	}
	components := strings.Split(relativePath, "/")
	parentFD := root.entry.fd
	parentOwned := false
	for index, component := range components {
		exact, err := nativeDirectoryContainsExactName(parentFD, component)
		if err != nil {
			if parentOwned {
				_ = unix.Close(parentFD)
			}
			return nil, err
		}
		if !exact {
			if parentOwned {
				_ = unix.Close(parentFD)
			}
			return nil, fmt.Errorf("artifact access path component %q does not exist with exact casing", component)
		}
		entry, err := openNativeChild(parentFD, component)
		if err != nil {
			if parentOwned {
				_ = unix.Close(parentFD)
			}
			return nil, err
		}
		if index == len(components)-1 {
			return &relativeNativeEntry{
				parentFD:    parentFD,
				parentOwned: parentOwned,
				name:        component,
				entry:       entry,
			}, nil
		}
		if entry.kind != nativeKindDirectory {
			operationErr := fmt.Errorf("artifact access path component %q is not a directory", component)
			operationErr = errors.Join(operationErr, unix.Close(entry.fd))
			if parentOwned {
				operationErr = errors.Join(operationErr, unix.Close(parentFD))
			}
			return nil, operationErr
		}
		if parentOwned {
			if closeErr := unix.Close(parentFD); closeErr != nil {
				return nil, errors.Join(closeErr, unix.Close(entry.fd))
			}
		}
		parentFD = entry.fd
		parentOwned = true
	}
	return nil, fmt.Errorf("artifact access path %q has no components", relativePath)
}

func (entry *relativeNativeEntry) verify() error {
	return verifyNativeEntry(entry.parentFD, entry.name, entry.entry)
}

func (entry *relativeNativeEntry) close() error {
	errorsToJoin := []error{unix.Close(entry.entry.fd)}
	if entry.parentOwned {
		errorsToJoin = append(errorsToJoin, unix.Close(entry.parentFD))
	}
	return errors.Join(errorsToJoin...)
}

func (root *nativeRoot) verify() error {
	var opened unix.Stat_t
	if err := unix.Fstat(root.entry.fd, &opened); err != nil {
		return err
	}
	if !root.entry.identity.equal(nativeIdentityFromStat(&opened)) {
		return fmt.Errorf("artifact access root changed while open")
	}
	if root.parentFD < 0 {
		return nil
	}
	observed, _, err := observeNativeEntry(root.parentFD, root.name)
	if err != nil {
		return err
	}
	if !root.entry.identity.equal(observed.identity) {
		return fmt.Errorf("artifact access root binding changed while open")
	}
	return nil
}

func (root *nativeRoot) close() error {
	errorsToJoin := []error{unix.Close(root.entry.fd)}
	if root.parentFD >= 0 {
		errorsToJoin = append(errorsToJoin, unix.Close(root.parentFD))
	}
	root.entry.fd = -1
	root.parentFD = -1
	return errors.Join(errorsToJoin...)
}

func openNativeChild(parentFD int, name string) (nativeEntry, error) {
	return openNativePathComponent(parentFD, name, true)
}

func openNativePathComponent(parentFD int, name string, requireStableMetadata bool) (nativeEntry, error) {
	observed, _, err := observeNativeEntry(parentFD, name)
	if err != nil {
		return nativeEntry{}, err
	}
	if observed.kind == nativeKindSymlink {
		return nativeEntry{}, fmt.Errorf("artifact access entry %q is a symbolic link; symlinks are not supported", name)
	}
	if observed.kind == nativeKindUnsupported {
		return nativeEntry{}, fmt.Errorf("artifact access entry %q has unsupported kind", name)
	}
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if observed.kind == nativeKindDirectory {
		flags |= unix.O_DIRECTORY
	}
	fd, err := unix.Openat(parentFD, name, flags, 0)
	if err != nil {
		return nativeEntry{}, err
	}
	opened, err := openedNativeEntry(fd)
	if err != nil {
		_ = unix.Close(fd)
		return nativeEntry{}, err
	}
	stable := observed.identity.sameBinding(opened.identity)
	if requireStableMetadata {
		stable = observed.identity.equal(opened.identity)
	}
	if !stable {
		_ = unix.Close(fd)
		return nativeEntry{}, fmt.Errorf("artifact access entry %q changed while opening", name)
	}
	return opened, nil
}

func openedNativeEntry(fd int) (nativeEntry, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return nativeEntry{}, err
	}
	return nativeEntryFromStat(fd, &stat), nil
}

func observeNativeEntry(parentFD int, name string) (nativeEntry, unix.Stat_t, error) {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nativeEntry{}, unix.Stat_t{}, err
	}
	return nativeEntryFromStat(-1, &stat), stat, nil
}

func verifyNativeEntry(parentFD int, name string, entry nativeEntry) error {
	if err := verifyNativeEntryBinding(parentFD, name, entry); err != nil {
		return err
	}
	exact, err := nativeDirectoryContainsExactName(parentFD, name)
	if err != nil {
		return err
	}
	if !exact {
		return fmt.Errorf("artifact access entry %q changed exact casing while open", name)
	}
	return nil
}

func verifyNativeEntryBinding(parentFD int, name string, entry nativeEntry) error {
	var opened unix.Stat_t
	if err := unix.Fstat(entry.fd, &opened); err != nil {
		return err
	}
	if !entry.identity.equal(nativeIdentityFromStat(&opened)) {
		return fmt.Errorf("artifact access entry %q changed while open", name)
	}
	observed, _, err := observeNativeEntry(parentFD, name)
	if err != nil {
		return err
	}
	if !entry.identity.equal(observed.identity) {
		return fmt.Errorf("artifact access entry %q binding changed while open", name)
	}
	return nil
}
