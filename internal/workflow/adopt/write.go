package adopt

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	adoptmodel "github.com/isty2e/daem/internal/adopt"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
)

type createdImportPath struct {
	path     string
	identity storagecommit.EntryIdentity
	revision mutation.SnapshotRevision
}

type createdImportDirectory struct {
	path     string
	identity os.FileInfo
}

func writePlan(ctx context.Context, plan adoptmodel.Plan, validateBeforeManifest func() error) (returnErr error) {
	if ctx == nil {
		return fmt.Errorf("import context is required")
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	sources := plan.Sources()
	skills := plan.Skills()
	output := plan.Output()
	manifestContent := plan.ManifestContent()
	created := make([]createdImportPath, 0, len(sources)+len(skills))
	createdDirectories, preparationErr := prepareImportParentDirectories(ctx, plan)
	cleanupOnFailure := true
	defer func() {
		if returnErr == nil || !cleanupOnFailure {
			return
		}
		cleanupErrors := make([]error, 0)
		cleanupContext := context.WithoutCancel(ctx)
		for index := len(created) - 1; index >= 0; index-- {
			_, err := os.Lstat(created[index].path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect imported path %q identity before cleanup: %w", created[index].path, err))
				continue
			}
			current, err := mutation.CaptureRevision(cleanupContext, mutation.RevisionRequest{
				Path: created[index].path, Effect: mutation.PathEffectDirectoryEntry,
			})
			if err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect imported path %q before cleanup: %w", created[index].path, err))
				continue
			}
			if !created[index].revision.Equal(current) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("imported path %q changed after creation; cleanup refused", created[index].path))
				continue
			}
			removal, err := storagecommit.NewLogicalRemoval(created[index].path, created[index].identity)
			if err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("prepare imported path %q rollback: %w", created[index].path, err))
				continue
			}
			if err := storagecommit.CommitLogicalRemoval(cleanupContext, removal); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("imported path %q was replaced after creation; cleanup refused: %w", created[index].path, err))
			}
		}
		for index := len(createdDirectories) - 1; index >= 0; index-- {
			currentIdentity, err := os.Lstat(createdDirectories[index].path)
			if err != nil {
				if !os.IsNotExist(err) {
					cleanupErrors = append(cleanupErrors, fmt.Errorf("inspect imported directory %q before cleanup: %w", createdDirectories[index].path, err))
				}
				continue
			}
			if !os.SameFile(createdDirectories[index].identity, currentIdentity) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("imported directory %q was replaced after creation; cleanup refused", createdDirectories[index].path))
				continue
			}
			if err := os.Remove(createdDirectories[index].path); err != nil && !os.IsNotExist(err) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove empty imported directory %q: %w", createdDirectories[index].path, err))
			}
		}
		returnErr = errors.Join(returnErr, errors.Join(cleanupErrors...))
	}()
	if preparationErr != nil {
		return preparationErr
	}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		createdPath, err := commitNewImportFile(ctx, source.SourcePath, source.Content, 0o600)
		if err != nil {
			return fmt.Errorf("write imported source %q: %w", source.SourcePath, err)
		}
		created = append(created, createdPath)
	}
	writtenSkillSources := make(map[string]struct{}, len(skills))
	for _, skill := range skills {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, written := writtenSkillSources[skill.SourcePath]; written {
			continue
		}
		writtenSkillSources[skill.SourcePath] = struct{}{}
		createdPath, err := copyImportedSkillDirectory(ctx, skill.ReadPath, skill.SourcePath)
		if err != nil {
			return fmt.Errorf("write imported skill source %q: %w", skill.SourcePath, err)
		}
		created = append(created, createdPath)
	}
	if validateBeforeManifest != nil {
		if err := validateBeforeManifest(); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.Merge() {
		if err := commitExistingImportFile(ctx, output, manifestContent); err != nil {
			if storageCommitMayBeVisible(err) {
				cleanupOnFailure = false
			}
			return fmt.Errorf("write output manifest %q: %w", output, err)
		}
	} else if _, err := commitNewFile(ctx, output, manifestContent, 0o600); err != nil {
		if storageCommitMayBeVisible(err) {
			cleanupOnFailure = false
		}
		return fmt.Errorf("write output manifest %q: %w", output, err)
	}

	return nil
}

func captureCreatedImportPath(ctx context.Context, path string) (createdImportPath, error) {
	identity, err := storagecommit.CaptureEntryIdentity(context.WithoutCancel(ctx), path)
	if err != nil {
		return createdImportPath{path: path}, err
	}
	revision, err := mutation.CaptureRevision(context.WithoutCancel(ctx), mutation.RevisionRequest{
		Path: path, Effect: mutation.PathEffectDirectoryEntry,
	})
	return createdImportPath{path: path, identity: identity, revision: revision}, err
}

func prepareImportParentDirectories(ctx context.Context, plan adoptmodel.Plan) ([]createdImportDirectory, error) {
	if err := plan.Validate(); err != nil {
		return nil, err
	}
	sources := plan.Sources()
	skills := plan.Skills()
	parents := make(map[string]struct{})
	commitPaths := make([]string, 0, 1+len(sources)+len(skills))
	addParents := func(path string) error {
		commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
		if err != nil {
			return fmt.Errorf("canonicalize import path %q: %w", path, err)
		}
		commitPaths = append(commitPaths, commitPath)
		for candidate := filepath.Dir(commitPath); ; candidate = filepath.Dir(candidate) {
			info, err := os.Stat(candidate)
			if err == nil {
				if !info.IsDir() {
					return fmt.Errorf("import parent path %q is not a directory", candidate)
				}
				return nil
			}
			if !os.IsNotExist(err) {
				return fmt.Errorf("inspect import parent path %q: %w", candidate, err)
			}
			parents[candidate] = struct{}{}
			parent := filepath.Dir(candidate)
			if parent == candidate {
				return fmt.Errorf("no existing ancestor for import path %q", commitPath)
			}
		}
	}
	if err := addParents(plan.Output()); err != nil {
		return nil, err
	}
	for _, source := range sources {
		if err := addParents(source.SourcePath); err != nil {
			return nil, err
		}
	}
	for _, skill := range skills {
		if err := addParents(skill.SourcePath); err != nil {
			return nil, err
		}
	}
	ordered := make([]string, 0, len(parents))
	for path := range parents {
		ordered = append(ordered, path)
	}
	sort.Slice(ordered, func(left int, right int) bool {
		if len(ordered[left]) == len(ordered[right]) {
			return ordered[left] < ordered[right]
		}
		return len(ordered[left]) < len(ordered[right])
	})
	for _, path := range commitPaths {
		if err := storagecommit.PrepareCommitParent(ctx, path); err != nil {
			created, captureErr := capturePreparedImportDirectories(context.WithoutCancel(ctx), ordered)
			return created, errors.Join(fmt.Errorf("prepare import parent for %q: %w", path, err), captureErr)
		}
	}
	return capturePreparedImportDirectories(ctx, ordered)
}

func capturePreparedImportDirectories(ctx context.Context, ordered []string) ([]createdImportDirectory, error) {
	created := make([]createdImportDirectory, 0, len(ordered))
	for _, path := range ordered {
		if err := ctx.Err(); err != nil {
			return created, err
		}
		identity, err := os.Lstat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return created, fmt.Errorf("inspect created import parent directory %q: %w", path, err)
		}
		created = append(created, createdImportDirectory{path: path, identity: identity})
	}
	return created, nil
}

func commitNewImportFile(ctx context.Context, path string, content []byte, mode os.FileMode) (createdImportPath, error) {
	commitPath, err := commitNewFile(ctx, path, content, mode)
	if err != nil {
		return createdImportPath{path: commitPath}, err
	}
	created, err := captureCreatedImportPath(ctx, commitPath)
	if err != nil {
		return created, importResidueError{path: commitPath, err: fmt.Errorf("capture committed import identity: %w", err)}
	}
	return created, nil
}

func commitNewFile(ctx context.Context, path string, content []byte, mode os.FileMode) (string, error) {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return "", err
	}
	request, err := storagecommit.NewFileCreate(commitPath, content, mode)
	if err != nil {
		return commitPath, err
	}
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return commitPath, err
	}
	return commitPath, nil
}

func commitExistingImportFile(ctx context.Context, path string, content []byte) error {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(commitPath)
	if err != nil {
		return err
	}
	expected, err := storagecommit.CaptureEntryIdentity(ctx, commitPath)
	if err != nil {
		return err
	}
	request, err := storagecommit.NewFileReplacement(commitPath, content, info.Mode().Perm(), expected)
	if err != nil {
		return err
	}
	return storagecommit.CommitFile(ctx, request)
}

func copyImportedSkillDirectory(ctx context.Context, sourceRoot string, destinationRoot string) (createdImportPath, error) {
	commitPath, err := mutation.CanonicalDirectoryEntryPath(destinationRoot)
	if err != nil {
		return createdImportPath{}, err
	}
	if exists, err := pathExists(commitPath); err != nil {
		return createdImportPath{}, fmt.Errorf("inspect imported skill destination %q: %w", destinationRoot, err)
	} else if exists {
		return createdImportPath{}, fmt.Errorf("imported skill destination already exists: %s", destinationRoot)
	}

	if err := storagecommit.PrepareCommitParent(ctx, commitPath); err != nil {
		return createdImportPath{}, err
	}
	destinationParent := filepath.Dir(commitPath)
	tempRoot, err := os.MkdirTemp(destinationParent, ".import-stage-")
	if err != nil {
		return createdImportPath{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(tempRoot)
		}
	}()
	if err := copySkillDirectoryContents(ctx, sourceRoot, tempRoot); err != nil {
		return createdImportPath{}, err
	}
	tempIdentity, err := storagecommit.CaptureEntryIdentity(ctx, tempRoot)
	if err != nil {
		return createdImportPath{}, err
	}
	request, err := storagecommit.NewPreparedTreeCommit(tempRoot, commitPath, tempIdentity)
	if err != nil {
		return createdImportPath{}, err
	}
	if err := storagecommit.CommitPreparedTree(ctx, request); err != nil {
		if storageCommitMayBeVisible(err) {
			committed = true
		}
		return createdImportPath{}, err
	}
	committed = true
	created, err := captureCreatedImportPath(ctx, commitPath)
	if err != nil {
		return created, importResidueError{path: commitPath, err: fmt.Errorf("capture committed import tree identity: %w", err)}
	}
	return created, nil
}

type importResidueError struct {
	path string
	err  error
}

func (err importResidueError) Error() string {
	return fmt.Sprintf("import commit at %q may be visible; residue retained: %v", err.path, err.err)
}

func (err importResidueError) Unwrap() error { return err.err }

func storageCommitMayBeVisible(err error) bool {
	var residue importResidueError
	if errors.As(err, &residue) {
		return true
	}
	kind, classified := mutationfs.FailureKindOf(err)
	return classified && kind == mutationfs.FailureIndeterminateCommit
}

func copySkillDirectoryContents(ctx context.Context, sourceRoot string, destinationRoot string) error {
	return filepath.WalkDir(sourceRoot, func(sourcePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relativePath, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(destinationRoot, relativePath)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(destinationPath, info.Mode().Perm())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("nested symlink %q is unsupported in imported skill", sourcePath)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		content, err := os.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o700); err != nil {
			return err
		}
		return os.WriteFile(destinationPath, content, info.Mode().Perm())
	})
}
