package artifact

import (
	"strings"
	"testing"
)

func TestFileMaterializationPreservesSourceIdentityAndNormalizesExecutableClass(t *testing.T) {
	content := []byte("#!/bin/sh\nexit 0\n")
	input := mustFileMaterializationIdentity(t, content, false)

	materialization, err := NewFileMaterialization(input, content, false, true)
	if err != nil {
		t.Fatalf("NewFileMaterialization returned error: %v", err)
	}
	if !materialization.InputIdentity().Equal(input) ||
		materialization.OutputIdentity().SourceID() != input.SourceID() ||
		materialization.OutputIdentity().ResolvedRef() != input.ResolvedRef() ||
		materialization.OutputIdentity().ContentHash() != HashFileContentWithExecutable(content, true) ||
		!materialization.Executable() ||
		!materialization.ChangesIdentity() ||
		!strings.HasPrefix(materialization.RecipeHash(), "sha256:") {
		t.Fatalf("materialization = %#v", materialization)
	}

	unchanged, err := NewFileMaterialization(input, content, false, false)
	if err != nil {
		t.Fatalf("NewFileMaterialization unchanged returned error: %v", err)
	}
	if unchanged.ChangesIdentity() || !unchanged.OutputIdentity().Equal(input) {
		t.Fatalf("unchanged materialization = %#v", unchanged)
	}
}

func TestFileMaterializationRejectsInvalidInputKindAndStaleBytes(t *testing.T) {
	content := []byte("content")
	directory, err := NewExactIdentity("local:directory", "", ArtifactKindDirectory, HashFileContent(content))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileMaterialization(directory, content, false, false); err == nil || !strings.Contains(err.Error(), "requires a file") {
		t.Fatalf("directory error = %v", err)
	}

	input := mustFileMaterializationIdentity(t, content, false)
	if _, err := NewFileMaterialization(input, []byte("changed"), false, true); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("stale bytes error = %v", err)
	}
	if _, err := NewFileMaterialization(input, content, true, true); err == nil || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("wrong source mode error = %v", err)
	}
}

func mustFileMaterializationIdentity(t *testing.T, content []byte, executable bool) ExactIdentity {
	t.Helper()
	identity, err := NewExactIdentity(
		"local:file",
		"",
		ArtifactKindFile,
		HashFileContentWithExecutable(content, executable),
	)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}
