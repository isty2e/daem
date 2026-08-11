package cache

import (
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestNewEntrySpecRejectsAmbiguousIdentityAndUnsafeContentPath(t *testing.T) {
	key := mustKey(t, "entry-spec", "value")
	for _, test := range []struct {
		name string
		path string
		hash artifact.ContentHash
		kind artifact.ArtifactKind
	}{
		{name: "empty path"},
		{name: "root path", path: "."},
		{name: "parent escape", path: "../content"},
		{name: "unclean path", path: "content/../file"},
		{name: "absolute path", path: "/content"},
		{name: "backslash", path: `content\file`},
		{name: "hash only", path: "content", hash: "sha256:hash"},
		{name: "kind only", path: "content", kind: artifact.ArtifactKindFile},
		{name: "unknown kind", path: "content", hash: "sha256:hash", kind: "archive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewEntrySpec(key, test.path, test.hash, test.kind); err == nil {
				t.Fatal("NewEntrySpec returned nil error")
			}
		})
	}
}

func TestCompletionRecordOwnershipIsNarrowerThanContentValidity(t *testing.T) {
	key := mustKey(t, "completion-owner", "value")
	wantHash := artifact.HashFileContent([]byte("wanted"))
	spec, err := NewEntrySpec(key, "content", wantHash, artifact.ArtifactKindFile)
	if err != nil {
		t.Fatal(err)
	}
	record := completionRecord{
		Version:     completionRecordVersion,
		Key:         key.PathComponent(),
		ContentPath: "content",
		ContentHash: artifact.HashFileContent([]byte("stale")),
		Kind:        artifact.ArtifactKindFile,
	}
	if err := record.validateOwnership(spec); err != nil {
		t.Fatalf("validateOwnership returned error: %v", err)
	}
	if err := record.validate(spec); err == nil {
		t.Fatal("validate accepted stale owned content identity")
	}
	record.Key = mustKey(t, "completion-owner", "other").PathComponent()
	if err := record.validateOwnership(spec); err == nil {
		t.Fatal("validateOwnership accepted another cache key")
	}
}
