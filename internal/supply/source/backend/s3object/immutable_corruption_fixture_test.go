package s3object

import (
	"bytes"
	"os"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

const immutableTestCompletionRecordName = ".daem-complete"

type immutableCorruptionFixture struct {
	resolver   Resolver
	client     *fakeS3Client
	sourceSpec sourcepkg.Source
	first      acquisition.Resolution
	identity   immutableLookupIdentity
	record     immutableLookupRecord
	rowRoot    string
}

func newImmutableCorruptionFixture(
	t *testing.T,
	cacheRoot string,
	uri string,
	requestedVersion string,
	body []byte,
) immutableCorruptionFixture {
	t.Helper()
	client := &fakeS3Client{body: body, versionID: requestedVersion}
	resolver, err := newResolverWithClient(cacheRoot, client)
	if err != nil {
		t.Fatal(err)
	}
	sourceSpec := sourcetest.S3(t, uri, requestedVersion, "", sourcepkg.S3ObjectFormatFile)
	first := mustResolveS3(t, resolver, sourceSpec)
	identity := mustImmutableLookupIdentity(t, first.Identity().SourceID(), requestedVersion)
	cacheAuthority := mustCaptureS3CacheRoot(t, resolver)
	record, found, err := resolver.state.immutableIndex.read(t.Context(), cacheAuthority, identity)
	if err != nil || !found {
		t.Fatalf("initial immutable row = found %t, error %v", found, err)
	}
	if err := cacheAuthority.Close(); err != nil {
		t.Fatalf("close cache authority: %v", err)
	}
	return immutableCorruptionFixture{
		resolver:   resolver,
		client:     client,
		sourceSpec: sourceSpec,
		first:      first,
		identity:   identity,
		record:     record,
		rowRoot:    mustImmutableLookupRoot(t, resolver.state.immutableIndex, identity),
	}
}

func assertImmutableFallbackRepairs(t *testing.T, fixture immutableCorruptionFixture) {
	t.Helper()
	second := mustResolveS3(t, fixture.resolver, fixture.sourceSpec)
	third := mustResolveS3(t, fixture.resolver, fixture.sourceSpec)
	firstPath := s3ResolutionContentPath(fixture.resolver, fixture.first)
	secondPath := s3ResolutionContentPath(fixture.resolver, second)
	if !second.Identity().Equal(fixture.first.Identity()) || secondPath != firstPath || third != second {
		t.Fatalf(
			"repaired artifact semantics differ or the new authority did not stabilize: first=%#v second=%#v third=%#v",
			fixture.first,
			second,
			third,
		)
	}
	if calls := fixture.client.callCount(); calls != 2 {
		t.Fatalf("GetObject calls = %d, want initial fetch plus one repair", calls)
	}
	cacheAuthority := mustCaptureS3CacheRoot(t, fixture.resolver)
	if _, found, err := fixture.resolver.state.immutableIndex.read(t.Context(), cacheAuthority, fixture.identity); err != nil || !found {
		t.Fatalf("repaired lookup row = found %t, error %v", found, err)
	}
	if err := cacheAuthority.Close(); err != nil {
		t.Fatalf("close cache authority: %v", err)
	}
}

func mustResolveS3(t *testing.T, resolver Resolver, sourceSpec sourcepkg.Source) acquisition.Resolution {
	t.Helper()
	resolved, err := resolver.Resolve(t.Context(), sourceSpec, noOperationOptions)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(content, want) {
		t.Fatalf("file %q content = %q, %v, want %q", path, content, err, want)
	}
}
