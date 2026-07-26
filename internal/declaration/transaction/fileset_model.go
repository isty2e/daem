// Package fileset commits a bounded set of exact files under recoverable
// before/after evidence. It owns publication mechanics, not file semantics.
package transaction

import (
	"fmt"
	"sort"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// FileTarget is one exact file transition in a recoverable transaction.
type FileTarget struct {
	path        string
	content     []byte
	write       bool
	commitPoint bool
}

// NewFileWrite constructs a target whose after-state is the supplied content.
func NewFileWrite(path string, content []byte) (FileTarget, error) {
	canonical, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return FileTarget{}, fmt.Errorf("canonicalize transaction target: %w", err)
	}
	return FileTarget{
		path:    canonical,
		content: append([]byte(nil), content...),
		write:   true,
	}, nil
}

// NewFileCommitPointWrite constructs the one after-image that may publish the
// transaction to observers outside the transaction's evidence authority. It is
// always written after every ordinary target.
func NewFileCommitPointWrite(path string, content []byte) (FileTarget, error) {
	target, err := NewFileWrite(path, content)
	if err != nil {
		return FileTarget{}, err
	}
	target.commitPoint = true
	return target, nil
}

// NewFileRetain constructs a target whose before-state must remain unchanged.
func NewFileRetain(path string) (FileTarget, error) {
	canonical, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return FileTarget{}, fmt.Errorf("canonicalize retained transaction target: %w", err)
	}
	return FileTarget{path: canonical}, nil
}

// Path returns the canonical directory-entry path.
func (target FileTarget) Path() string { return target.path }

func canonicalTargets(values []FileTarget) ([]FileTarget, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("file-set transaction requires at least one target")
	}
	targets := make([]FileTarget, len(values))
	commitPoints := 0
	seenPaths := make(map[string]struct{}, len(values))
	for index, value := range values {
		var (
			target FileTarget
			err    error
		)
		if value.write {
			target, err = NewFileWrite(value.path, value.content)
		} else {
			target, err = NewFileRetain(value.path)
		}
		if err != nil {
			return nil, fmt.Errorf("target[%d]: %w", index, err)
		}
		if _, exists := seenPaths[target.path]; exists {
			return nil, fmt.Errorf("file-set transaction target %q appears more than once", target.path)
		}
		seenPaths[target.path] = struct{}{}
		if value.commitPoint {
			if !value.write {
				return nil, fmt.Errorf("target[%d]: transaction commit point must write an after-image", index)
			}
			target.commitPoint = true
			commitPoints++
		}
		targets[index] = target
	}
	if commitPoints > 1 {
		return nil, fmt.Errorf("file-set transaction permits at most one commit point")
	}
	sort.Slice(targets, func(left int, right int) bool {
		if targets[left].commitPoint != targets[right].commitPoint {
			return !targets[left].commitPoint
		}
		return targets[left].path < targets[right].path
	})
	return targets, nil
}

func canonicalAllowedPaths(paths []string) (map[string]struct{}, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("file-set recovery requires allowed target paths")
	}
	allowed := make(map[string]struct{}, len(paths))
	for index, path := range paths {
		canonical, err := mutation.CanonicalDirectoryEntryPath(path)
		if err != nil {
			return nil, fmt.Errorf("allowed target[%d]: %w", index, err)
		}
		allowed[canonical] = struct{}{}
	}
	return allowed, nil
}
