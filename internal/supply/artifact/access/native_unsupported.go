//go:build !darwin && !linux

package access

import (
	"context"
	"io/fs"

	"github.com/isty2e/daem/internal/supply/artifact"
)

type directoryListingIdentity struct{}

type nativePathWitness struct{}

func (nativePathWitness) valid() bool { return false }

func inspectNative(_ string) (artifact.ArtifactKind, nativePathWitness, error) {
	return "", nativePathWitness{}, unavailableTraversal()
}

func readDirectoryNative(
	_ context.Context,
	_ View,
	_ string,
) ([]Entry, error) {
	return nil, unavailableTraversal()
}

func visitDirectoryNative(
	_ context.Context,
	_ View,
	_ string,
	_ func(Entry) error,
) error {
	return unavailableTraversal()
}

func visitDirectoryNamesNative(
	_ context.Context,
	_ View,
	_ string,
	_ func(string) error,
) (DirectoryListingWitness, error) {
	return DirectoryListingWitness{}, unavailableTraversal()
}

func verifyDirectoryListingNative(
	_ context.Context,
	_ View,
	_ string,
	_ DirectoryListingWitness,
) error {
	return unavailableTraversal()
}

func readFileNative(
	_ context.Context,
	_ View,
	_ string,
	_ int64,
) ([]byte, fs.FileMode, error) {
	return nil, 0, unavailableTraversal()
}

func walkNative(
	_ context.Context,
	_ View,
	_ TreeSink,
	_ *traversalBudget,
) (artifact.ContentHash, error) {
	return "", unavailableTraversal()
}

func unavailableTraversal() error {
	return ErrNoFollowTraversalUnavailable
}
