package s3object

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"

	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

var noOperationOptions acquisition.OperationOptions

func TestResolveS3FileObject(t *testing.T) {
	client := &fakeS3Client{
		body:      []byte("project instructions\n"),
		versionID: "requested-version",
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}

	resolvedArtifact, err := resolver.Resolve(context.Background(), sourcetest.S3(
		t,
		"s3://daem/instructions/project%20notes.md",
		"requested-version",
		"us-east-1",
		sourcepkg.S3ObjectFormatFile,
	), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if aws.ToString(client.input.Bucket) != "daem" {
		t.Fatalf("Bucket = %q, want daem", aws.ToString(client.input.Bucket))
	}
	if aws.ToString(client.input.Key) != "instructions/project notes.md" {
		t.Fatalf("Key = %q, want decoded key", aws.ToString(client.input.Key))
	}
	if aws.ToString(client.input.VersionId) != "requested-version" {
		t.Fatalf("VersionId = %q, want requested-version", aws.ToString(client.input.VersionId))
	}
	if resolvedArtifact.Identity().Kind() != artifact.ArtifactKindFile {
		t.Fatalf("Kind = %q, want file", resolvedArtifact.Identity().Kind())
	}
	if resolvedArtifact.Identity().ResolvedRef() != "requested-version" {
		t.Fatalf("ResolvedRef = %q, want requested-version", resolvedArtifact.Identity().ResolvedRef())
	}
	if resolvedArtifact.Identity().ContentHash() != artifact.HashFileContent([]byte("project instructions\n")) {
		t.Fatalf("ContentHash = %q, want file content hash", resolvedArtifact.Identity().ContentHash())
	}
	assertCompletionRecordOutsideContent(t, resolver, resolvedArtifact)

	content, err := resolvedArtifact.View().ReadRootFileVerified(t.Context(), resolvedArtifact.Identity(), 1<<20)
	if err != nil {
		t.Fatalf("ReadRootFileVerified returned error: %v", err)
	}
	if string(content.Bytes()) != "project instructions\n" {
		t.Fatalf("content = %q, want object body", content.Bytes())
	}
}

func TestResolveS3VersionCorrelationMatrix(t *testing.T) {
	for _, test := range []struct {
		name             string
		requestedVersion string
		responseVersion  string
		wantResolvedRef  artifact.ResolvedRef
	}{
		{
			name:             "explicit request with omitted response version",
			requestedVersion: "requested-version",
			wantResolvedRef:  "requested-version",
		},
		{name: "unversioned response", wantResolvedRef: ""},
		{
			name:            "unversioned request with server version",
			responseVersion: "server-version",
			wantResolvedRef: "server-version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &fakeS3Client{
				body:      []byte("version matrix\n"),
				versionID: test.responseVersion,
			}
			resolver, err := newResolverWithClient(t.TempDir(), client)
			if err != nil {
				t.Fatalf("newResolverWithClient returned error: %v", err)
			}
			sourceSpec := sourcetest.S3(
				t,
				"s3://daem/instructions/project.md",
				test.requestedVersion,
				"",
				sourcepkg.S3ObjectFormatFile,
			)

			resolved, err := resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
			if err != nil {
				t.Fatalf("Resolve returned error: %v", err)
			}
			if got := resolved.Identity().ResolvedRef(); got != test.wantResolvedRef {
				t.Fatalf("resolved ref = %q, want %q", got, test.wantResolvedRef)
			}
		})
	}
}

func TestResolveEmitsS3BackendEvents(t *testing.T) {
	client := &fakeS3Client{
		body:      []byte("project instructions\n"),
		versionID: "requested-version",
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(
		t,
		"s3://daem/instructions/project.md",
		"requested-version",
		"us-east-1",
		sourcepkg.S3ObjectFormatFile,
	)
	events := make([]acquisition.Event, 0)

	options := mustS3OperationOptions(t, sourceSpec, func(event acquisition.Event) {
		events = append(events, event)
	})
	resolvedArtifact, err := resolver.Resolve(context.Background(), sourceSpec, options)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if resolvedArtifact.Identity().ResolvedRef() != "requested-version" {
		t.Fatalf("ResolvedRef = %q, want requested-version", resolvedArtifact.Identity().ResolvedRef())
	}

	for _, want := range []acquisition.EventKind{
		acquisition.EventDownload,
		acquisition.EventHash,
		acquisition.EventCacheWait,
		acquisition.EventPublished,
	} {
		if !hasS3EventKind(events, want) {
			t.Fatalf("events = %#v, want %s", events, want)
		}
	}
	for _, event := range events {
		if event.Request().ID() != "instructions:000000" ||
			event.Request().Operation() != acquisition.OperationResolve ||
			event.Request().Source().Kind() != sourcepkg.SourceKindS3 {
			t.Fatalf("event = %#v, want request/source operation identity", event)
		}
	}
}

func TestResolveEmitsS3CacheHitOnlyForReusedArtifact(t *testing.T) {
	client := &fakeS3Client{
		body:      []byte("project instructions\n"),
		versionID: "requested-version",
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(
		t,
		"s3://daem/instructions/project.md",
		"requested-version",
		"us-east-1",
		sourcepkg.S3ObjectFormatFile,
	)
	request, err := acquisition.NewRequest(
		"instructions:000000",
		0,
		acquisition.OperationResolve,
		sourceSpec,
	)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	firstEvents := make([]acquisition.Event, 0)
	firstOptions, err := acquisition.NewOperationOptions(request, func(event acquisition.Event) {
		firstEvents = append(firstEvents, event)
	})
	if err != nil {
		t.Fatalf("NewOperationOptions returned error: %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), sourceSpec, firstOptions); err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	if hasS3EventKind(firstEvents, acquisition.EventCacheHit) {
		t.Fatalf("first events = %#v, want no cache_hit before artifact exists", firstEvents)
	}

	secondEvents := make([]acquisition.Event, 0)
	secondOptions, err := acquisition.NewOperationOptions(request, func(event acquisition.Event) {
		secondEvents = append(secondEvents, event)
	})
	if err != nil {
		t.Fatalf("NewOperationOptions returned error: %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), sourceSpec, secondOptions); err != nil {
		t.Fatalf("second Resolve returned error: %v", err)
	}
	if !hasS3EventKind(secondEvents, acquisition.EventCacheHit) {
		t.Fatalf("second events = %#v, want cache_hit for reused artifact", secondEvents)
	}
	if hasS3EventKind(secondEvents, acquisition.EventPublished) {
		t.Fatalf("second events = %#v, want reused cache without published", secondEvents)
	}
	if hasS3EventKind(secondEvents, acquisition.EventDownload) || hasS3EventKind(secondEvents, acquisition.EventHash) {
		t.Fatalf("second events = %#v, want verified reuse before download/hash", secondEvents)
	}
	if calls := client.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want one initial fetch", calls)
	}
}

func TestResolveS3TarGzipObject(t *testing.T) {
	client := &fakeS3Client{
		body:      tarGzipContent(t, []tarTestEntry{{name: "SKILL.md", content: "---\nname: oracle\n---\n"}}),
		versionID: "archive-version",
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}

	resolvedArtifact, err := resolver.Resolve(context.Background(), sourcetest.S3(
		t,
		"s3://daem/skills/oracle.tar.gz",
		"",
		"",
		sourcepkg.S3ObjectFormatTarGzip,
	), noOperationOptions)
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}

	if resolvedArtifact.Identity().Kind() != artifact.ArtifactKindDirectory {
		t.Fatalf("Kind = %q, want directory", resolvedArtifact.Identity().Kind())
	}
	if resolvedArtifact.Identity().ResolvedRef() != "archive-version" {
		t.Fatalf("ResolvedRef = %q, want archive-version", resolvedArtifact.Identity().ResolvedRef())
	}
	if _, err := resolvedArtifact.View().ReadFile(t.Context(), "SKILL.md", 1<<20); err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	assertCompletionRecordOutsideContent(t, resolver, resolvedArtifact)
}

func TestResolveS3ArchiveRejectsLinks(t *testing.T) {
	client := &fakeS3Client{
		body: tarGzipRaw(t, func(writer *tar.Writer) {
			if err := writer.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "target"}); err != nil {
				t.Fatalf("WriteHeader returned error: %v", err)
			}
		}),
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), sourcetest.S3(
		t,
		"s3://daem/skills/bad.tar.gz",
		"",
		"",
		sourcepkg.S3ObjectFormatTarGzip,
	), noOperationOptions)
	if err == nil {
		t.Fatal("Resolve returned nil error")
	}
	if !strings.Contains(err.Error(), "links are not supported") {
		t.Fatalf("error = %q, want link rejection", err)
	}

	sourceID := mustS3SourceID(t, sourcetest.S3(
		t,
		"s3://daem/skills/bad.tar.gz",
		"",
		"",
		sourcepkg.S3ObjectFormatTarGzip,
	))
	assertNoS3TempEntries(t, resolver.artifactParent(sourceID))
	assertNoS3CompletionRecords(t, resolver.artifactParent(sourceID))
}

func TestResolveRejectsEmptyS3Body(t *testing.T) {
	client := &fakeS3Client{nilBody: true}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), sourcetest.S3(
		t,
		"s3://daem/instructions/project.md",
		"",
		"",
		sourcepkg.S3ObjectFormatFile,
	), noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "empty response body") {
		t.Fatalf("Resolve error = %v, want empty body diagnostic", err)
	}
}

func TestResolveRejectsMismatchedExplicitVersionBeforePublication(t *testing.T) {
	bodyReader := &readCountingReader{reader: strings.NewReader("wrong version\n")}
	client := &fakeS3Client{
		body:      []byte("wrong version\n"),
		versionID: "returned-version",
		readerFactory: func([]byte) io.Reader {
			return bodyReader
		},
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(
		t,
		"s3://daem/instructions/project.md",
		"requested-version",
		"",
		sourcepkg.S3ObjectFormatFile,
	)

	_, err = resolver.Resolve(context.Background(), sourceSpec, noOperationOptions)
	if err == nil || !strings.Contains(err.Error(), "does not match requested version id") {
		t.Fatalf("Resolve error = %v, want explicit-version mismatch", err)
	}
	if bodyReader.reads != 0 {
		t.Fatalf("body reads = %d, want zero before correlation succeeds", bodyReader.reads)
	}
	if closes := client.closeCount(); closes != 1 {
		t.Fatalf("Body closes = %d, want 1", closes)
	}

	sourceID := mustS3SourceID(t, sourceSpec)
	assertNoS3TempEntries(t, resolver.artifactParent(sourceID))
	assertNoS3CompletionRecords(t, resolver.artifactParent(sourceID))
}

func TestResolveReportsS3ClientError(t *testing.T) {
	wantErr := errors.New("s3 unavailable")
	client := &fakeS3Client{err: wantErr}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}

	_, err = resolver.Resolve(context.Background(), sourcetest.S3(
		t,
		"s3://daem/instructions/project.md",
		"",
		"",
		sourcepkg.S3ObjectFormatFile,
	), noOperationOptions)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Resolve error = %v, want client error", err)
	}
	if !strings.Contains(err.Error(), "get s3 object s3://daem/instructions/project.md") {
		t.Fatalf("Resolve error = %q, want object context", err)
	}
}

func assertCompletionRecordOutsideContent(t *testing.T, resolver Resolver, resolved acquisition.Resolution) {
	t.Helper()

	identity := resolved.Identity()
	entryRoot := resolver.artifactEntryRoot(identity.SourceID(), identity.ResolvedRef(), identity.ContentHash())
	contentPath := s3ResolutionContentPath(resolver, resolved)
	if contentPath != filepath.Join(entryRoot, "content") {
		t.Fatalf("content path = %q, want entry content path", contentPath)
	}
	if !s3EntryExists(entryRoot) {
		t.Fatalf("entry root %q was not published", entryRoot)
	}

	info, err := os.Stat(contentPath)
	if err != nil {
		t.Fatalf("Stat ContentPath returned error: %v", err)
	}
	if !info.IsDir() {
		return
	}
	if _, err := os.Lstat(filepath.Join(contentPath, ".daem-complete")); !os.IsNotExist(err) {
		t.Fatalf("completion record is inside returned ContentPath or stat failed: %v", err)
	}
}

func hasS3EventKind(events []acquisition.Event, kind acquisition.EventKind) bool {
	for _, event := range events {
		if event.Kind() == kind {
			return true
		}
	}

	return false
}

func mustS3OperationOptions(
	t *testing.T,
	sourceSpec sourcepkg.Source,
	events acquisition.EventSink,
) acquisition.OperationOptions {
	t.Helper()
	request, err := acquisition.NewRequest(
		"instructions:000000",
		0,
		acquisition.OperationResolve,
		sourceSpec,
	)
	if err != nil {
		t.Fatalf("NewRequest returned error: %v", err)
	}
	options, err := acquisition.NewOperationOptions(request, events)
	if err != nil {
		t.Fatalf("NewOperationOptions returned error: %v", err)
	}
	return options
}
