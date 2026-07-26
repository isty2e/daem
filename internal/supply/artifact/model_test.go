package artifact

import (
	"strings"
	"testing"
)

func TestNewExactIdentityRejectsMalformedComponents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sourceID    SourceID
		resolvedRef ResolvedRef
		kind        ArtifactKind
		contentHash ContentHash
	}{
		{name: "empty source id", kind: ArtifactKindFile, contentHash: testExactContentHash("a")},
		{name: "source id whitespace", sourceID: " source", kind: ArtifactKindFile, contentHash: testExactContentHash("a")},
		{name: "source id invalid utf8", sourceID: SourceID("source\xff"), kind: ArtifactKindFile, contentHash: testExactContentHash("a")},
		{name: "resolved ref control", sourceID: "source", resolvedRef: "ref\n", kind: ArtifactKindFile, contentHash: testExactContentHash("a")},
		{name: "resolved ref invalid utf8", sourceID: "source", resolvedRef: ResolvedRef("ref\xff"), kind: ArtifactKindFile, contentHash: testExactContentHash("a")},
		{name: "unknown kind", sourceID: "source", kind: "archive", contentHash: testExactContentHash("a")},
		{name: "empty hash", sourceID: "source", kind: ArtifactKindFile},
		{name: "unqualified hash", sourceID: "source", kind: ArtifactKindFile, contentHash: "content"},
		{name: "empty digest", sourceID: "source", kind: ArtifactKindFile, contentHash: "sha256:"},
		{name: "nested digest delimiter", sourceID: "source", kind: ArtifactKindFile, contentHash: "sha256:a:b"},
		{name: "unsupported algorithm", sourceID: "source", kind: ArtifactKindFile, contentHash: ContentHash("sha512:" + strings.Repeat("a", 64))},
		{name: "short digest", sourceID: "source", kind: ArtifactKindFile, contentHash: "sha256:aa"},
		{name: "uppercase digest", sourceID: "source", kind: ArtifactKindFile, contentHash: ContentHash("sha256:" + strings.Repeat("A", 64))},
		{name: "non-hex digest", sourceID: "source", kind: ArtifactKindFile, contentHash: ContentHash("sha256:" + strings.Repeat("g", 64))},
		{name: "hash whitespace", sourceID: "source", kind: ArtifactKindFile, contentHash: ContentHash(" sha256:" + strings.Repeat("a", 64))},
		{name: "hash control", sourceID: "source", kind: ArtifactKindFile, contentHash: ContentHash("sha256:" + strings.Repeat("a", 63) + "\n")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewExactIdentity(test.sourceID, test.resolvedRef, test.kind, test.contentHash); err == nil {
				t.Fatal("NewExactIdentity() error = nil, want malformed identity rejection")
			}
		})
	}
}

func TestContentHashValidationDoesNotInventDigestSemantics(t *testing.T) {
	t.Parallel()

	allZero := ContentHash("sha256:" + strings.Repeat("0", 64))
	if err := allZero.Validate(); err != nil {
		t.Fatalf("ContentHash.Validate() error = %v for syntactically valid all-zero digest", err)
	}
}

func TestContentHashValidationAddsExactArtifactContextOnlyAtIdentityBoundary(t *testing.T) {
	t.Parallel()

	malformed := ContentHash("sha256:aa")
	hashErr := malformed.Validate()
	if hashErr == nil {
		t.Fatal("ContentHash.Validate() error = nil, want malformed digest rejection")
	}
	if !strings.Contains(hashErr.Error(), "content hash") {
		t.Fatalf("ContentHash.Validate() error = %q, want content hash context", hashErr)
	}
	if strings.Contains(hashErr.Error(), "exact artifact") {
		t.Fatalf("ContentHash.Validate() error = %q, must remain role-neutral", hashErr)
	}

	_, identityErr := NewExactIdentity("source", "", ArtifactKindFile, malformed)
	if identityErr == nil {
		t.Fatal("NewExactIdentity() error = nil, want malformed digest rejection")
	}
	if !strings.Contains(identityErr.Error(), "exact artifact content hash") {
		t.Fatalf("NewExactIdentity() error = %q, want exact artifact context", identityErr)
	}
}

func TestExactIdentityEqualityIncludesResolvedRefAndKind(t *testing.T) {
	t.Parallel()

	base, err := NewExactIdentity("source", "ref-a", ArtifactKindFile, testExactContentHash("a"))
	if err != nil {
		t.Fatalf("NewExactIdentity() error = %v", err)
	}
	same, err := NewExactIdentity("source", "ref-a", ArtifactKindFile, testExactContentHash("a"))
	if err != nil {
		t.Fatalf("NewExactIdentity() error = %v", err)
	}
	differentRef, err := NewExactIdentity("source", "ref-b", ArtifactKindFile, testExactContentHash("a"))
	if err != nil {
		t.Fatalf("NewExactIdentity() error = %v", err)
	}
	differentKind, err := NewExactIdentity("source", "ref-a", ArtifactKindDirectory, testExactContentHash("a"))
	if err != nil {
		t.Fatalf("NewExactIdentity() error = %v", err)
	}

	if !base.Equal(same) {
		t.Fatal("Equal() = false for identical values")
	}
	if base.Equal(differentRef) {
		t.Fatal("Equal() = true after resolved-ref change")
	}
	if base.Equal(differentKind) {
		t.Fatal("Equal() = true after kind change")
	}
}

func testExactContentHash(character string) ContentHash {
	return ContentHash("sha256:" + strings.Repeat(character, 64))
}
