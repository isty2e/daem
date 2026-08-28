//go:build darwin || linux

package access

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/supply/artifact"
	"golang.org/x/sys/unix"
)

const maximumNativePathComponents = 4096

func openNativeRoot(
	ctx context.Context,
	root string,
	expectedKind artifact.ArtifactKind,
	expectedAuthority nativePathWitness,
) (*nativeRoot, error) {
	if ctx == nil {
		return nil, fmt.Errorf("artifact access context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	components, err := nativeAbsolutePathComponents(root)
	if err != nil {
		return nil, err
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open artifact filesystem root: %w", err)
	}
	rootEntry, err := openedNativeEntry(rootFD)
	if err != nil {
		_ = unix.Close(rootFD)
		return nil, err
	}
	rootIdentity, err := nativePathComponentIdentityForFD(rootFD, rootEntry)
	if err != nil {
		return nil, errors.Join(err, unix.Close(rootFD))
	}
	authorityBuilder := newNativePathWitnessBuilder()
	authorityBuilder.append(rootIdentity)
	if len(components) == 0 {
		currentAuthority := authorityBuilder.finish()
		handle := &nativeRoot{
			path:      root,
			kind:      artifact.ArtifactKindDirectory,
			parentFD:  -1,
			entry:     rootEntry,
			authority: selectedNativePathAuthority(expectedAuthority, currentAuthority),
		}
		if expectedKind != "" {
			if err := handle.requireKind(expectedKind); err != nil {
				return nil, errors.Join(err, handle.close())
			}
		}
		if err := expectedAuthority.require(currentAuthority, "root"); err != nil {
			return nil, errors.Join(err, handle.close())
		}
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, handle.close())
		}
		return handle, nil
	}

	parentFD := rootFD
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, unix.Close(parentFD))
		}
		last := index == len(components)-1
		entry, err := openNativePathComponent(parentFD, component, last)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("open artifact root component %q: %w", component, err),
				unix.Close(parentFD),
			)
		}
		identity, err := nativePathComponentIdentityForFD(entry.fd, entry)
		if err != nil {
			return nil, errors.Join(err, unix.Close(entry.fd), unix.Close(parentFD))
		}
		authorityBuilder.append(identity)
		if last {
			currentAuthority := authorityBuilder.finish()
			handle := &nativeRoot{
				path:      root,
				kind:      artifactKindForNativeEntry(entry),
				name:      component,
				parentFD:  parentFD,
				entry:     entry,
				authority: selectedNativePathAuthority(expectedAuthority, currentAuthority),
			}
			if expectedKind != "" {
				if err := handle.requireKind(expectedKind); err != nil {
					return nil, errors.Join(err, handle.close())
				}
			}
			if err := expectedAuthority.require(currentAuthority, "root"); err != nil {
				return nil, errors.Join(err, handle.close())
			}
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(err, handle.close())
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
		if err := ctx.Err(); err != nil {
			return nil, errors.Join(err, unix.Close(entry.fd), unix.Close(parentFD))
		}
		if closeErr := unix.Close(parentFD); closeErr != nil {
			return nil, errors.Join(closeErr, unix.Close(entry.fd))
		}
		parentFD = entry.fd
	}
	_ = unix.Close(parentFD)
	return nil, fmt.Errorf("artifact access root %q has no path components", root)
}

func selectedNativePathAuthority(
	expected nativePathWitness,
	current nativePathWitness,
) nativePathWitness {
	if expected.valid() {
		return expected
	}
	return current
}

func artifactKindForNativeEntry(entry nativeEntry) artifact.ArtifactKind {
	if entry.kind == nativeKindDirectory {
		return artifact.ArtifactKindDirectory
	}
	return artifact.ArtifactKindFile
}

func nativeAbsolutePathComponents(root string) ([]string, error) {
	trimmed := strings.TrimPrefix(root, "/")
	if trimmed == "" {
		return nil, nil
	}
	return boundedNativePathComponents(trimmed)
}

func boundedNativePathComponents(value string) ([]string, error) {
	count := strings.Count(value, "/") + 1
	if count > maximumNativePathComponents {
		return nil, fmt.Errorf(
			"artifact access path exceeds %d components",
			maximumNativePathComponents,
		)
	}
	return strings.Split(value, "/"), nil
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
	rootFD       int
	relativePath string
	parentFD     int
	parentOwned  bool
	name         string
	entry        nativeEntry
	authority    nativePathWitness
}

func (root *nativeRoot) openRelative(
	ctx context.Context,
	relativePath string,
) (*relativeNativeEntry, error) {
	if relativePath == "." {
		return nil, nil
	}
	if root.entry.kind != nativeKindDirectory {
		return nil, fmt.Errorf("artifact file root has no child path %q", relativePath)
	}
	return openNativeRelative(ctx, root.entry.fd, relativePath, nativePathWitness{})
}

func openNativeRelative(
	ctx context.Context,
	rootFD int,
	relativePath string,
	expectedAuthority nativePathWitness,
) (*relativeNativeEntry, error) {
	return openNativeRelativeWithExactNameCheck(
		ctx,
		rootFD,
		relativePath,
		expectedAuthority,
		nativeDirectoryContainsExactName,
	)
}

func openNativeRelativeWithExactNameCheck(
	ctx context.Context,
	rootFD int,
	relativePath string,
	expectedAuthority nativePathWitness,
	containsExactName func(context.Context, int, string) (bool, error),
) (*relativeNativeEntry, error) {
	if ctx == nil {
		return nil, fmt.Errorf("artifact access context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	components, err := boundedNativePathComponents(relativePath)
	if err != nil {
		return nil, err
	}
	parentFD := rootFD
	parentOwned := false
	authorityBuilder := newNativePathWitnessBuilder()
	for index, component := range components {
		if err := ctx.Err(); err != nil {
			return nil, closeRelativeParent(err, parentFD, parentOwned)
		}
		entry, err := openNativeChild(parentFD, component)
		if err != nil {
			return nil, closeRelativeParent(err, parentFD, parentOwned)
		}
		if err := verifyNativeEntryWithExactNameCheck(
			ctx,
			parentFD,
			component,
			entry,
			containsExactName,
		); err != nil {
			return nil, errors.Join(
				err,
				unix.Close(entry.fd),
				closeRelativeParent(nil, parentFD, parentOwned),
			)
		}
		identity, err := nativePathComponentIdentityForFD(entry.fd, entry)
		if err != nil {
			return nil, errors.Join(
				err,
				unix.Close(entry.fd),
				closeRelativeParent(nil, parentFD, parentOwned),
			)
		}
		authorityBuilder.append(identity)
		if index == len(components)-1 {
			currentAuthority := authorityBuilder.finish()
			result := &relativeNativeEntry{
				rootFD:       rootFD,
				relativePath: relativePath,
				parentFD:     parentFD,
				parentOwned:  parentOwned,
				name:         component,
				entry:        entry,
				authority:    selectedNativePathAuthority(expectedAuthority, currentAuthority),
			}
			if err := expectedAuthority.require(currentAuthority, "relative"); err != nil {
				return nil, errors.Join(err, result.close())
			}
			if err := ctx.Err(); err != nil {
				return nil, errors.Join(err, result.close())
			}
			return result, nil
		}
		if entry.kind != nativeKindDirectory {
			operationErr := fmt.Errorf("artifact access path component %q is not a directory", component)
			return nil, errors.Join(
				operationErr,
				unix.Close(entry.fd),
				closeRelativeParent(nil, parentFD, parentOwned),
			)
		}
		if parentOwned {
			if closeErr := unix.Close(parentFD); closeErr != nil {
				return nil, errors.Join(closeErr, unix.Close(entry.fd))
			}
		}
		parentFD = entry.fd
		parentOwned = true
	}
	return nil, closeRelativeParent(
		fmt.Errorf("artifact access path %q has no components", relativePath),
		parentFD,
		parentOwned,
	)
}

func closeRelativeParent(operationErr error, parentFD int, parentOwned bool) error {
	if !parentOwned {
		return operationErr
	}
	return errors.Join(operationErr, unix.Close(parentFD))
}

func (entry *relativeNativeEntry) verify(ctx context.Context) error {
	if err := verifyNativeEntry(ctx, entry.parentFD, entry.name, entry.entry); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := openNativeRelative(ctx, entry.rootFD, entry.relativePath, entry.authority)
	if err != nil {
		return err
	}
	return errors.Join(current.close(), ctx.Err())
}

func (entry *relativeNativeEntry) close() error {
	errorsToJoin := []error{unix.Close(entry.entry.fd)}
	if entry.parentOwned {
		errorsToJoin = append(errorsToJoin, unix.Close(entry.parentFD))
	}
	return errors.Join(errorsToJoin...)
}

func (root *nativeRoot) verify(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("artifact access context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var opened unix.Stat_t
	if err := unix.Fstat(root.entry.fd, &opened); err != nil {
		return err
	}
	if !root.entry.identity.equal(nativeIdentityFromStat(&opened)) {
		return fmt.Errorf("artifact access root changed while open")
	}
	if root.parentFD >= 0 {
		observed, _, err := observeNativeEntry(root.parentFD, root.name)
		if err != nil {
			return err
		}
		if !root.entry.identity.equal(observed.identity) {
			return fmt.Errorf("artifact access root binding changed while open")
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := openNativeRoot(ctx, root.path, root.kind, root.authority)
	if err != nil {
		return err
	}
	return errors.Join(current.close(), ctx.Err())
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
		return nativeEntry{}, &unsupportedSymlinkError{path: name}
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

func verifyNativeEntry(
	ctx context.Context,
	parentFD int,
	name string,
	entry nativeEntry,
) error {
	return verifyNativeEntryWithExactNameCheck(
		ctx,
		parentFD,
		name,
		entry,
		nativeDirectoryContainsExactName,
	)
}

func verifyNativeEntryWithExactNameCheck(
	ctx context.Context,
	parentFD int,
	name string,
	entry nativeEntry,
	containsExactName func(context.Context, int, string) (bool, error),
) error {
	exact, err := containsExactName(ctx, parentFD, name)
	if err != nil {
		return err
	}
	if !exact {
		return fmt.Errorf("artifact access entry %q changed exact casing while open", name)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return verifyNativeEntryBinding(parentFD, name, entry)
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
