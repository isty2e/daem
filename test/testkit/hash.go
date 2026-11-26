package testkit

import (
	"context"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
)

func HashPath(t *testing.T, path string) string {
	t.Helper()

	return HashPathKind(t, path, artifact.ArtifactKindFile)
}

func HashDirectory(t *testing.T, path string) string {
	t.Helper()

	return HashPathKind(t, path, artifact.ArtifactKindDirectory)
}

func InstalledHash(t *testing.T, path string) string {
	t.Helper()
	view, err := access.OpenView(path)
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := view.PathHashes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return string(hashes.Installed)
}

func HashPathKind(t *testing.T, path string, wantKind artifact.ArtifactKind) string {
	t.Helper()

	contentHash, artifactKind, err := access.HashPath(context.Background(), path)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	if artifactKind != wantKind {
		t.Fatalf("artifactKind = %q, want %s", artifactKind, wantKind)
	}

	return string(contentHash)
}
