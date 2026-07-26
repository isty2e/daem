//go:build !darwin && !linux

package access

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func inspectNative(_ string) (artifact.ArtifactKind, error) {
	return "", unsupportedPlatform()
}

func readDirectoryNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ string,
) ([]Entry, error) {
	return nil, unsupportedPlatform()
}

func readFileNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ string,
	_ int64,
) ([]byte, fs.FileMode, error) {
	return nil, 0, unsupportedPlatform()
}

func walkNative(
	_ context.Context,
	_ string,
	_ artifact.ArtifactKind,
	_ TreeSink,
	_ *traversalBudget,
) (artifact.ContentHash, error) {
	return "", unsupportedPlatform()
}

func unsupportedPlatform() error {
	return fmt.Errorf("artifact access no-follow traversal is unsupported on this platform")
}
