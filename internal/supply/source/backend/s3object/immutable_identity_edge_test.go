package s3object

import (
	"os"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveExactVersionSeparatesCanonicalSourceAxes(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		first  sourcepkg.Source
		second sourcepkg.Source
	}{
		{
			name:   "region",
			body:   []byte("same object\n"),
			first:  sourcetest.S3(t, "s3://daem/object", "v1", "us-east-1", sourcepkg.S3ObjectFormatFile),
			second: sourcetest.S3(t, "s3://daem/object", "v1", "eu-west-1", sourcepkg.S3ObjectFormatFile),
		},
		{
			name: "format",
			body: tarGzipContent(t, []tarTestEntry{{name: "SKILL.md", content: "---\nname: edge\n---\n"}}),
			first: sourcetest.S3(
				t,
				"s3://daem/object",
				"v1",
				"us-east-1",
				sourcepkg.S3ObjectFormatFile,
			),
			second: sourcetest.S3(
				t,
				"s3://daem/object",
				"v1",
				"us-east-1",
				sourcepkg.S3ObjectFormatTarGzip,
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeS3Client{
				bodies:     [][]byte{test.body, test.body},
				versionIDs: []string{"v1", "v1"},
			}
			resolver, err := newResolverWithClient(t.TempDir(), client)
			if err != nil {
				t.Fatal(err)
			}

			first, err := resolver.Resolve(t.Context(), test.first)
			if err != nil {
				t.Fatalf("first Resolve returned error: %v", err)
			}
			second, err := resolver.Resolve(t.Context(), test.second)
			if err != nil {
				t.Fatalf("second Resolve returned error: %v", err)
			}
			reused, err := resolver.Resolve(t.Context(), test.first)
			if err != nil {
				t.Fatalf("reused Resolve returned error: %v", err)
			}

			if first.Identity().SourceID() == second.Identity().SourceID() ||
				s3ResolutionContentPath(resolver, first) == s3ResolutionContentPath(resolver, second) {
				t.Fatalf("canonical source axes collided: first=%#v second=%#v", first, second)
			}
			if reused != first {
				t.Fatalf("reused artifact = %#v, want %#v", reused, first)
			}
			if test.name == "format" &&
				(first.Identity().Kind() != artifact.ArtifactKindFile || second.Identity().Kind() != artifact.ArtifactKindDirectory) {
				t.Fatalf("format kinds = %q/%q, want file/directory", first.Identity().Kind(), second.Identity().Kind())
			}
			if calls := client.callCount(); calls != 2 {
				t.Fatalf("GetObject calls = %d, want one per canonical source identity", calls)
			}
		})
	}
}

func TestResolveVersionlessResponseVersionDoesNotEnablePersistentReuse(t *testing.T) {
	cacheRoot := t.TempDir()
	client := &fakeS3Client{
		bodies:     [][]byte{[]byte("first\n"), []byte("second\n")},
		versionIDs: []string{"server-v1", "server-v2"},
	}
	resolver, err := newResolverWithClient(cacheRoot, client)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "", "us-east-1", sourcepkg.S3ObjectFormatFile)

	first, err := resolver.Resolve(t.Context(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	second, err := resolver.Resolve(t.Context(), sourceSpec)
	if err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}

	if first.Identity().ResolvedRef() != "server-v1" || second.Identity().ResolvedRef() != "server-v2" {
		t.Fatalf(
			"returned refs = %q/%q, want server-v1/server-v2",
			first.Identity().ResolvedRef(),
			second.Identity().ResolvedRef(),
		)
	}
	if first.Identity().ContentHash() == second.Identity().ContentHash() {
		t.Fatalf("versionless sequential resolves reused content hash %q", first.Identity().ContentHash())
	}
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("GetObject calls = %d, want sequential refetch", calls)
	}
	if _, err := os.Lstat(resolver.state.immutableIndex.root); !os.IsNotExist(err) {
		t.Fatalf("versionless resolve created immutable index state: %v", err)
	}
}
