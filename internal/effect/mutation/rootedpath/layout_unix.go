//go:build darwin || linux

package rootedpath

import (
	"errors"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

func capturePathLayoutPlatform(path string, referent bool, maximumPhysicalDepth int, budget PhysicalTraversalBudget) (layout PathLayout, returnErr error) {
	absolute, err := canonicalPathOnlyDestinationPath(path)
	if err != nil {
		return PathLayout{}, err
	}
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return PathLayout{}, err
	}
	traversal.captureAliases = true
	physical, platform, suffix, err := resolveLayoutPath(absolute, referent, traversal)
	if err != nil {
		return PathLayout{}, err
	}
	defer func() {
		returnErr = errors.Join(returnErr, closeCapturedRootPlatform(&platform))
		if returnErr != nil {
			layout = PathLayout{}
		}
	}()
	anchor, err := captureLayoutAnchor(platform.directories[len(platform.directories)-1])
	if err != nil {
		return PathLayout{}, err
	}
	resolved := filepath.Join(physical, filepath.Join(suffix...))
	depth, err := absolutePathDepth(resolved)
	if err != nil {
		return PathLayout{}, err
	}
	if err := traversal.validateResolvedDepth(depth); err != nil {
		return PathLayout{}, err
	}
	return PathLayout{path: resolved, anchor: anchor, aliases: slices.Clone(traversal.aliases), referent: referent}, nil
}

func captureLayoutAnchor(directory capturedDirectory) (layoutAnchor, error) {
	object, err := nativeObjectToken(directory.fd, directory.device, directory.inode)
	if err != nil {
		return layoutAnchor{}, err
	}
	return layoutAnchor{path: directory.path, object: object, mount: directory.mount}, nil
}

func resolvePathLayoutPlatform(layout PathLayout, maximumPhysicalDepth int, budget PathObservationBudget) (string, []string, error) {
	traversal, err := newPhysicalTraversal(maximumPhysicalDepth, budget)
	if err != nil {
		return "", nil, err
	}
	traversal.rejectAliases = true
	if err := validateLayoutAnchor(layout.anchor, traversal); err != nil {
		return "", nil, err
	}
	for _, alias := range layout.aliases {
		if err := validateLayoutAlias(alias, traversal, budget); err != nil {
			return "", nil, err
		}
	}
	physical, platform, suffix, err := resolveLayoutPath(layout.path, layout.referent, traversal)
	if err != nil {
		return "", nil, err
	}
	if err := closeCapturedRootPlatform(&platform); err != nil {
		return "", nil, err
	}
	return physical, suffix, nil
}

func resolveLayoutPath(path string, referent bool, traversal *physicalTraversal) (string, capturedRootPlatform, []string, error) {
	var physical string
	var platform capturedRootPlatform
	var suffix []string
	var err error
	if referent {
		physical, platform, _, _, suffix, err = resolveNativePathPlatform(path, true, false, true, traversal)
	} else {
		physical, platform, _, _, suffix, err = resolveDirectoryPathPlatform(filepath.Dir(path), true, false, traversal)
		suffix = append(suffix, filepath.Base(path))
	}
	if err != nil {
		return "", capturedRootPlatform{}, nil, errors.Join(err, closeCapturedRootPlatform(&platform))
	}
	return physical, platform, suffix, nil
}

func validateLayoutAnchor(anchor layoutAnchor, traversal *physicalTraversal) (returnErr error) {
	platform, err := openLayoutAnchor(anchor.path, traversal)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeCapturedRootPlatform(&platform)) }()
	current, err := captureLayoutAnchor(platform.directories[len(platform.directories)-1])
	if err != nil {
		return err
	}
	if current != anchor {
		return newFailure(FailureRootReplaced, anchor.path, "prepared path parent changed", nil)
	}
	return nil
}

func validateLayoutAlias(alias layoutAlias, traversal *physicalTraversal, budget PathObservationBudget) (returnErr error) {
	platform, err := openLayoutAnchor(alias.parent.path, traversal)
	if err != nil {
		return err
	}
	defer func() { returnErr = errors.Join(returnErr, closeCapturedRootPlatform(&platform)) }()
	parent := platform.directories[len(platform.directories)-1]
	anchor, err := captureLayoutAnchor(parent)
	if err != nil {
		return err
	}
	if anchor != alias.parent {
		return newFailure(FailureRootReplaced, alias.parent.path, "prepared alias parent changed", nil)
	}
	path := filepath.Join(alias.parent.path, alias.name)
	if err := traversal.budget.AdmitPathComponents(1); err != nil {
		return err
	}
	if err := admitLayoutAliasRead(budget, alias); err != nil {
		return err
	}
	buffer := make([]byte, len(alias.target)+1)
	count, err := unix.Readlinkat(parent.fd, alias.name, buffer)
	if err != nil {
		return newFailure(FailureRootReplaced, path, "observe prepared alias", err)
	}
	if string(buffer[:count]) != alias.target {
		return newFailure(FailureRootReplaced, path, "prepared alias layout changed", nil)
	}
	return nil
}

func openLayoutAnchor(path string, traversal *physicalTraversal) (capturedRootPlatform, error) {
	_, platform, _, _, _, err := resolveDirectoryPathPlatform(path, false, false, traversal)
	if err != nil {
		return capturedRootPlatform{}, errors.Join(err, closeCapturedRootPlatform(&platform))
	}
	return platform, err
}
