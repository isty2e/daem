//go:build darwin || linux

package mutation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"golang.org/x/sys/unix"
)

var leaseNamespaceComponents = [...]string{"locks", "mutation", "v1"}

type openedLeaseDirectory struct {
	parent *os.File
	file   *os.File
	name   string
	path   string
}

type unixLeaseNamespace struct {
	directories []openedLeaseDirectory
	boundary    rootedpath.DirectoryMountBoundary
	rootPath    string
	rootKey     string
	rootWitness pathSemanticsWitness
	closed      bool
	mu          sync.Mutex
}

type unixLeaseRecord struct {
	file *os.File
}

func initialLeaseRootIdentity(dataDir string, root string) (canonicalPath, error) {
	relative, err := filepath.Rel(dataDir, root)
	if err != nil || relative != filepath.Join(leaseNamespaceComponents[:]...) {
		return canonicalPath{}, fmt.Errorf("mutation lease root %q is not below data directory %q", root, dataDir)
	}
	candidate := dataDir
	for _, component := range leaseNamespaceComponents {
		candidate = filepath.Join(candidate, component)
		info, inspectErr := os.Lstat(candidate)
		if errors.Is(inspectErr, os.ErrNotExist) {
			break
		}
		if inspectErr != nil {
			return canonicalPath{}, fmt.Errorf("inspect mutation lease namespace %q: %w", candidate, inspectErr)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return canonicalPath{}, fmt.Errorf("mutation lease namespace %q is not a regular directory", candidate)
		}
	}
	return canonicalPathIdentity(root, PathEffectReferent)
}

func (store Store) openLeaseNamespace() (preparedLeaseNamespace, error) {
	if strings.TrimSpace(store.dataDir) == "" || strings.TrimSpace(store.root) == "" ||
		store.rootKey == "" || store.rootWitness == "" || store.maximum <= 0 || store.interval <= 0 {
		return nil, fmt.Errorf("mutation lease store is not initialized")
	}

	dataDirectory, err := openOrCreateLeaseDataDirectory(store.dataDir)
	if err != nil {
		return nil, err
	}
	boundary, err := rootedpath.CaptureDirectoryMountBoundary(dataDirectory.Fd())
	if err != nil {
		_ = dataDirectory.Close()
		return nil, fmt.Errorf("capture mutation lease data-root mount: %w", err)
	}
	namespace := &unixLeaseNamespace{
		directories: []openedLeaseDirectory{{file: dataDirectory, path: store.dataDir}},
		boundary:    boundary,
		rootPath:    store.root,
		rootKey:     store.rootKey,
		rootWitness: store.rootWitness,
	}
	for _, component := range leaseNamespaceComponents {
		parent := namespace.directories[len(namespace.directories)-1].file
		path := filepath.Join(namespace.directories[len(namespace.directories)-1].path, component)
		child, childErr := openOrCreateLeaseNamespaceDirectory(parent, component, path, boundary)
		if childErr != nil {
			_ = namespace.Close()
			return nil, childErr
		}
		namespace.directories = append(namespace.directories, openedLeaseDirectory{
			parent: parent,
			file:   child,
			name:   component,
			path:   path,
		})
	}
	if err := namespace.ValidateCurrent(); err != nil {
		_ = namespace.Close()
		return nil, err
	}
	return namespace, nil
}

func (store Store) prepare() error {
	namespace, err := store.openLeaseNamespace()
	if err != nil {
		return err
	}
	return namespace.Close()
}

func openOrCreateLeaseDataDirectory(path string) (*os.File, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Dir(path) == path {
		return nil, fmt.Errorf("mutation data directory %q must be an absolute clean non-root path", path)
	}
	rootFD, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root for mutation data directory: %w", err)
	}
	current := os.NewFile(uintptr(rootFD), "/")
	if current == nil {
		_ = unix.Close(rootFD)
		return nil, fmt.Errorf("bind filesystem root descriptor for mutation data directory")
	}
	currentPath := string(filepath.Separator)
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	for index, component := range components {
		if component == "" {
			_ = current.Close()
			return nil, fmt.Errorf("mutation data directory %q contains an empty path component", path)
		}
		last := index == len(components)-1
		if last {
			if err := validateLeaseDirectoryMetadata(currentPath, current, false); err != nil {
				_ = current.Close()
				return nil, fmt.Errorf("validate mutation data parent: %w", err)
			}
		}
		nextPath := filepath.Join(currentPath, component)
		next, created, openErr := openOrCreateLeaseDirectory(current, component, nextPath)
		_ = current.Close()
		if openErr != nil {
			return nil, openErr
		}
		if created {
			if err := validateLeaseDirectoryMetadata(nextPath, next, true); err != nil {
				_ = next.Close()
				return nil, err
			}
		}
		current = next
		currentPath = nextPath
	}
	if err := validateLeaseDirectoryMetadata(path, current, false); err != nil {
		_ = current.Close()
		return nil, err
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		_ = current.Close()
		return nil, fmt.Errorf("verify mutation data directory %q: %w", path, err)
	}
	openedInfo, err := current.Stat()
	if err != nil {
		_ = current.Close()
		return nil, fmt.Errorf("inspect opened mutation data directory %q: %w", path, err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(pathInfo, openedInfo) {
		_ = current.Close()
		return nil, fmt.Errorf("mutation data directory %q changed while opening it", path)
	}
	return current, nil
}

func openOrCreateLeaseNamespaceDirectory(
	parent *os.File,
	name string,
	path string,
	boundary rootedpath.DirectoryMountBoundary,
) (*os.File, error) {
	child, _, err := openOrCreateLeaseDirectory(parent, name, path)
	if err != nil {
		return nil, err
	}
	if err := validateLeaseDirectoryMetadata(path, child, true); err != nil {
		_ = child.Close()
		return nil, err
	}
	if err := boundary.ValidateDirectoryHandle(child.Fd()); err != nil {
		_ = child.Close()
		return nil, fmt.Errorf("validate mutation lease namespace mount for %q: %w", path, err)
	}
	return child, nil
}

func openOrCreateLeaseDirectory(
	parent *os.File,
	name string,
	path string,
) (*os.File, bool, error) {
	var before unix.Stat_t
	created := false
	err := unix.Fstatat(int(parent.Fd()), name, &before, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		if mkdirErr := unix.Mkdirat(int(parent.Fd()), name, 0o700); mkdirErr != nil {
			if !errors.Is(mkdirErr, unix.EEXIST) {
				return nil, false, fmt.Errorf("create mutation lease directory %q: %w", path, mkdirErr)
			}
		} else {
			created = true
		}
		err = unix.Fstatat(int(parent.Fd()), name, &before, unix.AT_SYMLINK_NOFOLLOW)
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect mutation lease directory %q: %w", path, err)
	}
	if before.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, false, fmt.Errorf("mutation lease directory %q is not a regular directory", path)
	}
	fd, err := unix.Openat(
		int(parent.Fd()),
		name,
		unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		0,
	)
	if err != nil {
		return nil, false, fmt.Errorf("open mutation lease directory %q: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, false, fmt.Errorf("bind mutation lease directory descriptor %q", path)
	}
	var opened unix.Stat_t
	if err := unix.Fstat(fd, &opened); err != nil {
		_ = file.Close()
		return nil, false, fmt.Errorf("inspect opened mutation lease directory %q: %w", path, err)
	}
	if !sameLeaseObject(before, opened) {
		_ = file.Close()
		return nil, false, fmt.Errorf("mutation lease directory %q changed while opening it", path)
	}
	return file, created, nil
}

func validateLeaseDirectoryMetadata(path string, file *os.File, private bool) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect mutation lease directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("mutation lease path %q is not a regular directory", path)
	}
	if err := validateLeaseEntryOwner(path, info); err != nil {
		return err
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("mutation lease directory %q is group/world-writable with mode %04o", path, info.Mode().Perm())
	}
	if private && info.Mode().Perm() != 0o700 {
		if err := file.Chmod(0o700); err != nil {
			return fmt.Errorf("set private mutation lease directory mode for %q: %w", path, err)
		}
		secured, err := file.Stat()
		if err != nil {
			return fmt.Errorf("verify private mutation lease directory %q: %w", path, err)
		}
		if secured.Mode().Perm() != 0o700 {
			return fmt.Errorf("mutation lease directory %q did not retain private mode", path)
		}
	}
	return nil
}

func (namespace *unixLeaseNamespace) Acquire(
	ctx context.Context,
	name string,
	access AccessMode,
	interval time.Duration,
) (leaseRecord, bool, error) {
	if ctx == nil {
		return nil, false, fmt.Errorf("mutation lease context is required")
	}
	if err := access.validate(); err != nil {
		return nil, false, err
	}
	if interval <= 0 {
		return nil, false, fmt.Errorf("mutation lease retry interval must be positive")
	}
	if err := namespace.ValidateCurrent(); err != nil {
		return nil, false, err
	}
	root, err := namespace.rootDirectory()
	if err != nil {
		return nil, false, err
	}
	record, err := openLeaseRecord(root, name, filepath.Join(namespace.rootPath, name))
	if err != nil {
		return nil, false, err
	}
	key, recordID, err := bindOpenedLeaseRecord(root, name, record.file)
	if err != nil {
		_ = record.close()
		return nil, false, err
	}
	lease, locked, err := processLeaseRegistry.acquire(ctx, key, recordID, record, access, interval)
	if err != nil || !locked {
		return nil, locked, err
	}
	if err := namespace.ValidateCurrent(); err != nil {
		return nil, false, errors.Join(err, lease.Unlock())
	}
	if err := validateCurrentLeaseRecord(root, name, recordID); err != nil {
		return nil, false, errors.Join(err, lease.Unlock())
	}
	return lease, true, nil
}

func openLeaseRecord(root *os.File, name string, path string) (*unixLeaseRecord, error) {
	var before unix.Stat_t
	var existed bool
	var fd int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		before = unix.Stat_t{}
		existed = true
		err = unix.Fstatat(int(root.Fd()), name, &before, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			existed = false
		} else if err != nil {
			return nil, fmt.Errorf("inspect mutation lock record %q: %w", path, err)
		} else if before.Mode&unix.S_IFMT != unix.S_IFREG {
			return nil, fmt.Errorf("mutation lock record %q has unsupported file mode", path)
		}
		fd, err = unix.Openat(
			int(root.Fd()),
			name,
			unix.O_RDWR|unix.O_CREAT|unix.O_CLOEXEC|unix.O_NOFOLLOW,
			0o600,
		)
		if !errors.Is(err, unix.ENOENT) || attempt == 2 {
			break
		}
		runtime.Gosched()
	}
	if err != nil {
		return nil, fmt.Errorf("open mutation lock record %q: %w", path, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind mutation lock record descriptor %q", path)
	}
	if existed {
		var opened unix.Stat_t
		if err := unix.Fstat(fd, &opened); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("inspect opened mutation lock record %q: %w", path, err)
		}
		if !sameLeaseObject(before, opened) {
			_ = file.Close()
			return nil, fmt.Errorf("mutation lock record %q changed while opening it", path)
		}
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("inspect mutation lock record %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("mutation lock record %q has unsupported file mode %s", path, info.Mode())
	}
	if err := validateLeaseEntryOwner(path, info); err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = file.Close()
		return nil, fmt.Errorf("mutation lock record permissions %04o are not private", info.Mode().Perm())
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("set private mutation lock record mode: %w", err)
	}
	secured, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("verify private mutation lock record mode: %w", err)
	}
	if secured.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, fmt.Errorf("mutation lock record %q did not retain private mode", path)
	}
	return &unixLeaseRecord{file: file}, nil
}

func acquireUnixFlock(
	ctx context.Context,
	file *os.File,
	access AccessMode,
	interval time.Duration,
) (bool, error) {
	operation := unix.LOCK_EX | unix.LOCK_NB
	if access == AccessShared {
		operation = unix.LOCK_SH | unix.LOCK_NB
	}
	for {
		err := unix.Flock(int(file.Fd()), operation)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return false, err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

func bindOpenedLeaseRecord(
	root *os.File,
	name string,
	file *os.File,
) (unixProcessLeaseKey, unixLeaseObjectID, error) {
	rootID, err := leaseObjectID(root)
	if err != nil {
		return unixProcessLeaseKey{}, unixLeaseObjectID{}, fmt.Errorf(
			"inspect mutation lease namespace descriptor: %w",
			err,
		)
	}
	recordID, err := leaseObjectID(file)
	if err != nil {
		return unixProcessLeaseKey{}, unixLeaseObjectID{}, fmt.Errorf(
			"inspect opened mutation lock descriptor: %w",
			err,
		)
	}
	if err := validateCurrentLeaseRecord(root, name, recordID); err != nil {
		return unixProcessLeaseKey{}, unixLeaseObjectID{}, err
	}
	return unixProcessLeaseKey{directory: rootID, name: name}, recordID, nil
}

func validateCurrentLeaseRecord(root *os.File, name string, want unixLeaseObjectID) error {
	var pathStat unix.Stat_t
	if err := unix.Fstatat(int(root.Fd()), name, &pathStat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("inspect acquired mutation lock record: %w", err)
	}
	got := unixLeaseObjectID{device: uint64(pathStat.Dev), inode: uint64(pathStat.Ino)}
	if pathStat.Mode&unix.S_IFMT != unix.S_IFREG || got != want {
		return fmt.Errorf("mutation lock record changed while acquiring it")
	}
	return nil
}

func (namespace *unixLeaseNamespace) ValidateCurrent() error {
	if namespace == nil {
		return fmt.Errorf("mutation lease namespace is not initialized")
	}
	namespace.mu.Lock()
	defer namespace.mu.Unlock()
	if namespace.closed || len(namespace.directories) != len(leaseNamespaceComponents)+1 {
		return fmt.Errorf("mutation lease namespace is not active")
	}
	data := namespace.directories[0]
	pathInfo, err := os.Lstat(data.path)
	if err != nil {
		return fmt.Errorf("inspect selected mutation data root %q: %w", data.path, err)
	}
	openedInfo, err := data.file.Stat()
	if err != nil {
		return fmt.Errorf("inspect retained mutation data root %q: %w", data.path, err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(pathInfo, openedInfo) {
		return fmt.Errorf("selected mutation data root %q changed", data.path)
	}
	for _, directory := range namespace.directories[1:] {
		var current unix.Stat_t
		if err := unix.Fstatat(
			int(directory.parent.Fd()),
			directory.name,
			&current,
			unix.AT_SYMLINK_NOFOLLOW,
		); err != nil {
			return fmt.Errorf("inspect mutation lease namespace %q: %w", directory.path, err)
		}
		var retained unix.Stat_t
		if err := unix.Fstat(int(directory.file.Fd()), &retained); err != nil {
			return fmt.Errorf("inspect retained mutation lease namespace %q: %w", directory.path, err)
		}
		if current.Mode&unix.S_IFMT != unix.S_IFDIR || !sameLeaseObject(current, retained) {
			return fmt.Errorf("mutation lease namespace %q changed", directory.path)
		}
		if err := namespace.boundary.ValidateDirectoryHandle(directory.file.Fd()); err != nil {
			return fmt.Errorf("validate mutation lease namespace mount for %q: %w", directory.path, err)
		}
	}
	identity, err := canonicalPathIdentity(namespace.rootPath, PathEffectReferent)
	if err != nil {
		return fmt.Errorf("observe current mutation lease root: %w", err)
	}
	if identity.keyPath != namespace.rootKey || identity.witness != namespace.rootWitness {
		return fmt.Errorf("mutation lease store identity changed")
	}
	return nil
}

func (namespace *unixLeaseNamespace) rootDirectory() (*os.File, error) {
	namespace.mu.Lock()
	defer namespace.mu.Unlock()
	if namespace.closed || len(namespace.directories) == 0 {
		return nil, fmt.Errorf("mutation lease namespace is not active")
	}
	return namespace.directories[len(namespace.directories)-1].file, nil
}

func (namespace *unixLeaseNamespace) Close() error {
	if namespace == nil {
		return nil
	}
	namespace.mu.Lock()
	defer namespace.mu.Unlock()
	if namespace.closed {
		return nil
	}
	namespace.closed = true
	var failures []error
	for index := len(namespace.directories) - 1; index >= 0; index-- {
		if err := namespace.directories[index].file.Close(); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func sameLeaseObject(left unix.Stat_t, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino
}
