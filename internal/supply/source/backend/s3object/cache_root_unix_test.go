//go:build darwin || linux

package s3object

import (
	"os"
	"path/filepath"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestNewResolverRejectsSymlinkedSelectedCacheRoot(t *testing.T) {
	parent := t.TempDir()
	physicalRoot := filepath.Join(parent, "physical")
	if err := os.Mkdir(physicalRoot, 0o700); err != nil {
		t.Fatalf("create physical cache root: %v", err)
	}
	selectedRoot := filepath.Join(parent, "selected")
	if err := os.Symlink(physicalRoot, selectedRoot); err != nil {
		t.Fatalf("create selected cache-root symlink: %v", err)
	}

	if _, err := NewResolver(selectedRoot); err == nil {
		t.Fatal("NewResolver accepted a symlinked selected cache root")
	}
	assertEmptyS3Directory(t, physicalRoot)
}

func TestResolveRejectsSelectedCacheRootReplacedBySymlink(t *testing.T) {
	parent := t.TempDir()
	cacheRoot := filepath.Join(parent, "cache")
	if err := os.Mkdir(cacheRoot, 0o700); err != nil {
		t.Fatalf("create selected cache root: %v", err)
	}
	client := &fakeS3Client{body: []byte("confined\n")}
	resolver, err := newResolverWithClient(cacheRoot, client)
	if err != nil {
		t.Fatalf("newResolverWithClient returned error: %v", err)
	}
	movedRoot := filepath.Join(parent, "cache-moved")
	if err := os.Rename(cacheRoot, movedRoot); err != nil {
		t.Fatalf("move selected cache root: %v", err)
	}
	external := t.TempDir()
	if err := os.Symlink(external, cacheRoot); err != nil {
		t.Fatalf("replace selected cache root with symlink: %v", err)
	}
	sourceSpec := sourcetest.S3(
		t,
		"s3://daem/confined",
		"",
		"",
		sourcepkg.S3ObjectFormatFile,
	)

	if _, err := resolver.Resolve(t.Context(), sourceSpec, noOperationOptions); err == nil {
		t.Fatal("Resolve accepted a selected cache root replaced by a symlink")
	}
	if calls := client.callCount(); calls != 0 {
		t.Fatalf("GetObject calls = %d, want no network work", calls)
	}
	assertEmptyS3Directory(t, external)
}

func TestResolveRejectsPreexistingSymlinkedCacheNamespaces(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		versionID string
	}{
		{name: "artifact namespace", namespace: "artifacts"},
		{name: "immutable index namespace", namespace: filepath.Join("indexes", "s3-immutable"), versionID: "v1"},
		{name: "immutable lock namespace", namespace: filepath.Join("locks", "s3-immutable"), versionID: "v1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			external := t.TempDir()
			namespace := filepath.Join(cacheRoot, test.namespace)
			if err := os.MkdirAll(filepath.Dir(namespace), 0o700); err != nil {
				t.Fatalf("create namespace parent: %v", err)
			}
			if err := os.Symlink(external, namespace); err != nil {
				t.Fatalf("create namespace symlink: %v", err)
			}
			client := &fakeS3Client{body: []byte("confined\n"), versionID: test.versionID}
			resolver, err := newResolverWithClient(cacheRoot, client)
			if err != nil {
				t.Fatalf("newResolverWithClient returned error: %v", err)
			}
			sourceSpec := sourcetest.S3(
				t,
				"s3://daem/confined",
				test.versionID,
				"",
				sourcepkg.S3ObjectFormatFile,
			)

			if _, err := resolver.Resolve(t.Context(), sourceSpec, noOperationOptions); err == nil {
				t.Fatal("Resolve accepted a symlinked cache namespace")
			}
			if calls := client.callCount(); calls != 0 {
				t.Fatalf("GetObject calls = %d, want no network work", calls)
			}
			assertEmptyS3Directory(t, external)
		})
	}
}

func TestResolveRejectsCacheNamespaceReplacementAfterRootCapture(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		versionID string
		wantCalls int
	}{
		{name: "artifact namespace", namespace: "artifacts", wantCalls: 1},
		{name: "immutable index namespace", namespace: filepath.Join("indexes", "s3-immutable"), versionID: "v1"},
		{name: "immutable lock namespace", namespace: filepath.Join("locks", "s3-immutable"), versionID: "v1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cacheRoot := t.TempDir()
			external := t.TempDir()
			namespace := filepath.Join(cacheRoot, test.namespace)
			if err := os.MkdirAll(namespace, 0o700); err != nil {
				t.Fatalf("create initial namespace: %v", err)
			}
			client := &fakeS3Client{body: []byte("confined\n"), versionID: test.versionID}
			resolver, err := newResolverWithClient(cacheRoot, client)
			if err != nil {
				t.Fatalf("newResolverWithClient returned error: %v", err)
			}
			var replacementErr error
			resolver.state.testAfterCacheRootCapture = func() {
				moved := namespace + ".moved"
				if err := os.Rename(namespace, moved); err != nil {
					replacementErr = err
					return
				}
				replacementErr = os.Symlink(external, namespace)
			}
			sourceSpec := sourcetest.S3(
				t,
				"s3://daem/confined",
				test.versionID,
				"",
				sourcepkg.S3ObjectFormatFile,
			)

			if _, err := resolver.Resolve(t.Context(), sourceSpec, noOperationOptions); err == nil {
				t.Fatal("Resolve accepted a cache namespace replaced after root capture")
			}
			if replacementErr != nil {
				t.Fatalf("replace cache namespace: %v", replacementErr)
			}
			if calls := client.callCount(); calls != test.wantCalls {
				t.Fatalf("GetObject calls = %d, want %d", calls, test.wantCalls)
			}
			assertEmptyS3Directory(t, external)
		})
	}
}

func TestResolveDoesNotRefetchAfterImmutableArtifactAuthorityLoss(t *testing.T) {
	cacheRoot := t.TempDir()
	fixture := newImmutableCorruptionFixture(
		t,
		cacheRoot,
		"s3://daem/object",
		"v1",
		[]byte("trusted\n"),
	)
	artifactRoot := filepath.Join(cacheRoot, "artifacts")
	movedRoot := artifactRoot + ".moved"
	if err := os.Rename(artifactRoot, movedRoot); err != nil {
		t.Fatalf("move artifact namespace: %v", err)
	}
	external := t.TempDir()
	if err := os.Symlink(external, artifactRoot); err != nil {
		t.Fatalf("replace artifact namespace: %v", err)
	}

	if _, err := fixture.resolver.Resolve(t.Context(), fixture.sourceSpec, noOperationOptions); err == nil {
		t.Fatal("Resolve accepted immutable artifact authority loss")
	}
	if calls := fixture.client.callCount(); calls != 1 {
		t.Fatalf("GetObject calls = %d, want no refetch after authority loss", calls)
	}
	assertEmptyS3Directory(t, external)
}

func assertEmptyS3Directory(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read directory %q: %v", root, err)
	}
	if len(entries) != 0 {
		t.Fatalf("directory %q entries = %v, want none", root, entries)
	}
}
