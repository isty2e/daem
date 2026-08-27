//go:build !darwin && !linux

package access

import (
	"context"
	"io/fs"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func inspectNative(_ string) (artifact.ArtifactKind, error) {
	return "", unavailableTraversal()
}

func readDirectoryNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ string,
) ([]Entry, error) {
	return nil, unavailableTraversal()
}

func visitDirectoryNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ string,
	_ func(Entry) error,
) error {
	return unavailableTraversal()
}

func visitDirectoryNamesNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ string,
	_ func(string) error,
) error {
	return unavailableTraversal()
}

func readFileNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ string,
	_ int64,
) ([]byte, fs.FileMode, error) {
	return nil, 0, unavailableTraversal()
}

func walkNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ TreeSink,
	_ *traversalBudget,
) (artifact.ContentHash, error) {
	return "", unavailableTraversal()
}

func unavailableTraversal() error {
	return ErrNoFollowTraversalUnavailable
}
