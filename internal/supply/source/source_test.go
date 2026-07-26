package source

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestSourceIDForGitSource(t *testing.T) {
	sourceSpec := mustGitSource(
		t,
		"https://github.com/Standigm/agent-commons.git",
		"skills/.curated/pdf",
		"main",
	)
	sourceID, err := SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	want := artifact.SourceID("git:locator=https%3A%2F%2Fgithub.com%2FStandigm%2Fagent-commons.git&path=skills%2F.curated%2Fpdf&ref=name%3Amain")
	if sourceID != want {
		t.Fatalf("sourceID = %q, want %q", sourceID, want)
	}
}

func TestSourceIDForLocalSource(t *testing.T) {
	sourceID, err := SourceIDFor(mustLocalSource(
		t,
		"local-skills/technical-deck-visual-qa",
		LocalSourceModeVendor,
	))
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	want := artifact.SourceID("local:local-skills/technical-deck-visual-qa?mode=vendor")
	if sourceID != want {
		t.Fatalf("sourceID = %q, want %q", sourceID, want)
	}
}

func TestSourceIDForS3Source(t *testing.T) {
	sourceID, err := SourceIDFor(mustS3Source(
		t,
		"s3://daem/skills/oracle.tar.gz",
		"version/1",
		"us-east-1",
		S3ObjectFormatTarGzip,
	))
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	want := artifact.SourceID("s3:s3://daem/skills/oracle.tar.gz?format=tar.gz&region=us-east-1&version_id=version%2F1")
	if sourceID != want {
		t.Fatalf("sourceID = %q, want %q", sourceID, want)
	}
}

func TestParseS3ObjectURIRejectsPrefixSources(t *testing.T) {
	_, err := ParseS3ObjectURI("s3://daem/skills/")
	if err == nil {
		t.Fatal("ParseS3ObjectURI returned nil error")
	}

	if !strings.Contains(err.Error(), "prefix directory sources are unsupported") {
		t.Fatalf("error = %q, want prefix directory diagnostic", err)
	}
}

func TestParseS3ObjectURISeparatesCanonicalAndObjectKey(t *testing.T) {
	parsed, err := ParseS3ObjectURI("s3://daem/instructions/project%20notes.md")
	if err != nil {
		t.Fatalf("ParseS3ObjectURI returned error: %v", err)
	}

	if parsed.Key() != "instructions/project notes.md" {
		t.Fatalf("Key = %q, want decoded object key", parsed.Key())
	}
	if parsed.Canonical() != "s3://daem/instructions/project%20notes.md" {
		t.Fatalf("Canonical = %q, want escaped canonical URI", parsed.Canonical())
	}
}

func TestSourceIDForCleansLocalSourcePaths(t *testing.T) {
	localSource := mustLocalSource(
		t,
		"./local-skills/../local-skills/demo",
		LocalSourceModeLink,
	)
	canonical := mustLocalSource(t, "local-skills/demo", LocalSourceModeLink)
	if localSource != canonical {
		t.Fatalf("local sources differ after canonical construction: %#v != %#v", localSource, canonical)
	}

	localSourceID, err := SourceIDFor(localSource)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}

	if localSourceID != artifact.SourceID("local:local-skills/demo?mode=link") {
		t.Fatalf("localSourceID = %q", localSourceID)
	}
}

func TestLocalAndS3ConstructorsRejectInvalidCanonicalFacts(t *testing.T) {
	if _, err := NewLocalSource("", LocalSourceModeVendor); err == nil {
		t.Fatal("NewLocalSource accepted an empty path")
	}
	if _, err := NewLocalSource("skills/demo", LocalSourceMode("mirror")); err == nil {
		t.Fatal("NewLocalSource accepted an unknown mode")
	}
	if _, err := NewLocalSource("skills/\x00demo", LocalSourceModeVendor); err == nil {
		t.Fatal("NewLocalSource accepted a NUL path")
	}
	if _, err := NewS3Source("https://bucket/key", "", "", S3ObjectFormatFile); err == nil {
		t.Fatal("NewS3Source accepted a non-S3 URI")
	}
	if _, err := NewS3Source("s3://bucket/key", "", "", S3ObjectFormat("zip")); err == nil {
		t.Fatal("NewS3Source accepted an unknown format")
	}
}

func TestS3ConstructorStoresCanonicalFacts(t *testing.T) {
	encoded := mustS3Source(t, "s3://bucket/project%20notes.md", " version-1 ", " us-east-1 ", "")
	plain := mustS3Source(t, "s3://bucket/project notes.md", "version-1", "us-east-1", S3ObjectFormatFile)
	if encoded != plain {
		t.Fatalf("S3 sources differ after canonical construction: %#v != %#v", encoded, plain)
	}

	sourceID, err := SourceIDFor(encoded)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}
	want := artifact.SourceID("s3:s3://bucket/project%20notes.md?region=us-east-1&version_id=version-1")
	if sourceID != want {
		t.Fatalf("sourceID = %q, want %q", sourceID, want)
	}
}

func TestS3ConstructorCanonicalizesEquivalentPathEscapes(t *testing.T) {
	tests := []struct {
		name      string
		left      string
		right     string
		canonical string
	}{
		{
			name:      "escaped slash",
			left:      "s3://bucket/a%2Fb",
			right:     "s3://bucket/a/b",
			canonical: "s3://bucket/a/b",
		},
		{
			name:      "escaped unreserved character",
			left:      "s3://bucket/%41",
			right:     "s3://bucket/A",
			canonical: "s3://bucket/A",
		},
		{
			name:      "escape hex case",
			left:      "s3://bucket/a%2fb",
			right:     "s3://bucket/a%2Fb",
			canonical: "s3://bucket/a/b",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := mustS3Source(t, test.left, "", "", S3ObjectFormatFile)
			right := mustS3Source(t, test.right, "", "", S3ObjectFormatFile)
			if left != right {
				t.Fatalf("equivalent S3 sources differ: %#v != %#v", left, right)
			}

			s3Source, ok := left.S3()
			if !ok {
				t.Fatal("canonical source is not S3-backed")
			}
			if got := s3Source.ObjectURI().Canonical(); got != test.canonical {
				t.Fatalf("Canonical = %q, want %q", got, test.canonical)
			}

			leftID, err := SourceIDFor(left)
			if err != nil {
				t.Fatalf("SourceIDFor(left) returned error: %v", err)
			}
			rightID, err := SourceIDFor(right)
			if err != nil {
				t.Fatalf("SourceIDFor(right) returned error: %v", err)
			}
			if leftID != rightID {
				t.Fatalf("equivalent source ids differ: %q != %q", leftID, rightID)
			}
		})
	}
}

func TestS3ConstructorPreservesDistinctDecodedKeys(t *testing.T) {
	tests := []struct {
		name          string
		left          string
		right         string
		leftCanonical string
	}{
		{
			name:          "encoded percent versus slash",
			left:          "s3://bucket/a%252Fb",
			right:         "s3://bucket/a%2Fb",
			leftCanonical: "s3://bucket/a%252Fb",
		},
		{
			name:          "dot segment is object data",
			left:          "s3://bucket/a/../b",
			right:         "s3://bucket/b",
			leftCanonical: "s3://bucket/a/../b",
		},
		{
			name:          "repeated slash is object data",
			left:          "s3://bucket/a//b",
			right:         "s3://bucket/a/b",
			leftCanonical: "s3://bucket/a//b",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			left := mustS3Source(t, test.left, "", "", S3ObjectFormatFile)
			right := mustS3Source(t, test.right, "", "", S3ObjectFormatFile)
			if left == right {
				t.Fatal("distinct decoded S3 keys collapsed to one source")
			}

			s3Source, ok := left.S3()
			if !ok {
				t.Fatal("canonical source is not S3-backed")
			}
			if got := s3Source.ObjectURI().Canonical(); got != test.leftCanonical {
				t.Fatalf("Canonical = %q, want %q", got, test.leftCanonical)
			}
		})
	}
}

func mustGitSource(t *testing.T, locator string, repositoryPath string, ref string) Source {
	t.Helper()

	sourceSpec, err := NewGitSource(locator, repositoryPath, ref)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return sourceSpec
}

func mustLocalSource(t *testing.T, path string, mode LocalSourceMode) Source {
	t.Helper()
	value, err := NewLocalSource(path, mode)
	if err != nil {
		t.Fatalf("NewLocalSource returned error: %v", err)
	}
	return value
}

func mustS3Source(
	t *testing.T,
	uri string,
	versionID string,
	region string,
	format S3ObjectFormat,
) Source {
	t.Helper()
	value, err := NewS3Source(uri, versionID, region, format)
	if err != nil {
		t.Fatalf("NewS3Source returned error: %v", err)
	}
	return value
}

func TestSourceIDForInvalidSource(t *testing.T) {
	_, err := SourceIDFor(Source{})
	if err == nil {
		t.Fatal("SourceIDFor returned nil error")
	}

	if !strings.Contains(err.Error(), "unsupported source kind") {
		t.Fatalf("error = %q, want unsupported source kind", err)
	}
}
