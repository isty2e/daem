package s3object

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/supply/artifact"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveExactVersionReusesVerifiedArtifactAcrossResolverInstances(t *testing.T) {
	cacheRoot := t.TempDir()
	fakeClient := &fakeS3Client{body: []byte("immutable\n"), versionID: "requested-version"}
	var factoryMu sync.Mutex
	factoryCalls := 0
	factory := func(context.Context, clientConfiguration) (client, error) {
		factoryMu.Lock()
		factoryCalls++
		factoryMu.Unlock()
		return fakeClient, nil
	}
	resolver, err := newResolverWithClientFactory(cacheRoot, factory)
	if err != nil {
		t.Fatalf("newResolverWithClientFactory returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(
		t,
		"s3://daem/instructions/project.md",
		"requested-version",
		"us-east-1",
		sourcepkg.S3ObjectFormatFile,
	)

	first, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	identity := mustImmutableLookupIdentity(t, first.Identity().SourceID(), "requested-version")
	if _, found, err := resolver.state.immutableIndex.read(context.Background(), identity); err != nil || !found {
		t.Fatalf("published lookup read = found %t, error %v", found, err)
	}
	copied := resolver
	second, err := copied.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("copied Resolve returned error: %v", err)
	}
	independent, err := newResolverWithClientFactory(cacheRoot, factory)
	if err != nil {
		t.Fatalf("independent newResolverWithClientFactory returned error: %v", err)
	}
	third, err := independent.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("independent Resolve returned error: %v", err)
	}

	if first != second || first != third {
		t.Fatalf("resolved artifacts differ: first=%#v second=%#v third=%#v", first, second, third)
	}
	factoryMu.Lock()
	gotFactoryCalls := factoryCalls
	factoryMu.Unlock()
	if gotFactoryCalls != 1 {
		t.Fatalf("client factory calls = %d, want one initial resolution", gotFactoryCalls)
	}
	if calls := fakeClient.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want one initial resolution", calls)
	}
}

func TestResolveExactVersionKeysDifferentRequestedVersionsSeparately(t *testing.T) {
	client := &fakeS3Client{
		bodies:     [][]byte{[]byte("version one\n"), []byte("version two\n")},
		versionIDs: []string{"requested-one", "requested-two"},
	}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	firstSource := sourcetest.S3(t, "s3://daem/object", "requested-one", "", sourcepkg.S3ObjectFormatFile)
	secondSource := sourcetest.S3(t, "s3://daem/object", "requested-two", "", sourcepkg.S3ObjectFormatFile)

	first, err := resolver.Resolve(context.Background(), firstSource)
	if err != nil {
		t.Fatalf("first version Resolve returned error: %v", err)
	}
	second, err := resolver.Resolve(context.Background(), secondSource)
	if err != nil {
		t.Fatalf("second version Resolve returned error: %v", err)
	}
	reused, err := resolver.Resolve(context.Background(), firstSource)
	if err != nil {
		t.Fatalf("reused first version Resolve returned error: %v", err)
	}

	if first.Identity().ContentHash() == second.Identity().ContentHash() ||
		first.Identity().SourceID() == second.Identity().SourceID() {
		t.Fatalf("different requested versions collided: first=%#v second=%#v", first, second)
	}
	if reused != first {
		t.Fatalf("reused artifact = %#v, want %#v", reused, first)
	}
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("GetObject calls = %d, want one call per requested version", calls)
	}
}

func TestResolveRepairsCorruptImmutableLookupRecord(t *testing.T) {
	cacheRoot := t.TempDir()
	client := &fakeS3Client{body: []byte("trusted\n"), versionID: "v1"}
	resolver, err := newResolverWithClient(cacheRoot, client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	first, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	identity := mustImmutableLookupIdentity(t, first.Identity().SourceID(), "v1")
	entryRoot, err := resolver.state.immutableIndex.entryRoot(identity)
	if err != nil {
		t.Fatalf("entryRoot returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(entryRoot, immutableLookupRecordName), []byte("not json\n"), 0o600); err != nil {
		t.Fatalf("corrupt lookup record: %v", err)
	}

	second, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("fallback Resolve returned error: %v", err)
	}
	third, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("post-repair Resolve returned error: %v", err)
	}
	if second != first || third != first {
		t.Fatalf("artifacts after repair differ: first=%#v second=%#v third=%#v", first, second, third)
	}
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("GetObject calls = %d, want initial fetch plus corruption fallback", calls)
	}
	if _, found, err := resolver.state.immutableIndex.read(context.Background(), identity); err != nil || !found {
		t.Fatalf("repaired lookup read = found %t, error %v", found, err)
	}
}

func TestResolveRepairsWrongModeImmutableLookupRecord(t *testing.T) {
	client := &fakeS3Client{body: []byte("trusted\n"), versionID: "v1"}
	resolver, err := newResolverWithClient(t.TempDir(), client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	first, err := resolver.Resolve(context.Background(), sourceSpec)
	if err != nil {
		t.Fatalf("first Resolve returned error: %v", err)
	}
	identity := mustImmutableLookupIdentity(t, first.Identity().SourceID(), "v1")
	entryRoot, err := resolver.state.immutableIndex.entryRoot(identity)
	if err != nil {
		t.Fatalf("entryRoot returned error: %v", err)
	}
	recordPath := filepath.Join(entryRoot, immutableLookupRecordName)
	if err := os.Chmod(recordPath, 0o644); err != nil {
		t.Fatalf("chmod lookup record: %v", err)
	}

	if _, err := resolver.Resolve(context.Background(), sourceSpec); err != nil {
		t.Fatalf("fallback Resolve returned error: %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), sourceSpec); err != nil {
		t.Fatalf("post-repair Resolve returned error: %v", err)
	}
	info, err := os.Stat(recordPath)
	if err != nil {
		t.Fatalf("Stat repaired record returned error: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("repaired record mode = %04o, want 0600", info.Mode().Perm())
	}
	if calls := client.callCount(); calls != 2 {
		t.Fatalf("GetObject calls = %d, want initial fetch plus mode-repair fallback", calls)
	}
}

func TestResolveCanceledWhileWaitingForImmutableLookupDoesNotCreateClient(t *testing.T) {
	cacheRoot := t.TempDir()
	fakeClient := &fakeS3Client{body: []byte("unused\n"), versionID: "v1"}
	factoryCalls := 0
	resolver, err := newResolverWithClientFactory(cacheRoot, func(context.Context, clientConfiguration) (client, error) {
		factoryCalls++
		return fakeClient, nil
	})
	if err != nil {
		t.Fatalf("newResolverWithClientFactory returned error: %v", err)
	}
	sourceSpec := sourcetest.S3(t, "s3://daem/object", "v1", "", sourcepkg.S3ObjectFormatFile)
	sourceID := mustS3SourceID(t, sourceSpec)
	identity := mustImmutableLookupIdentity(t, sourceID, "v1")
	heldLock, err := resolver.state.immutableIndex.acquire(context.Background(), identity)
	if err != nil {
		t.Fatalf("acquire held lookup lock: %v", err)
	}
	defer heldLock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = resolver.Resolve(ctx, sourceSpec)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Resolve error = %v, want context deadline", err)
	}
	if factoryCalls != 0 || fakeClient.callCount() != 0 {
		t.Fatalf("client factory/GetObject calls = %d/%d, want no network work", factoryCalls, fakeClient.callCount())
	}
}

func mustImmutableLookupIdentity(
	t testing.TB,
	sourceID artifact.SourceID,
	versionID string,
) immutableLookupIdentity {
	t.Helper()
	identity, eligible, err := newImmutableLookupIdentity(sourceID, versionID)
	if err != nil || !eligible {
		t.Fatalf("newImmutableLookupIdentity = %#v, %t, %v", identity, eligible, err)
	}
	return identity
}
