package s3object

import (
	"os"
	"path/filepath"
	"testing"

	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
)

func TestResolveRejectsCorruptReferencedArtifactsAndRepairs(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, immutableCorruptionFixture) func(*testing.T)
	}{
		{name: "missing root", mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			if err := os.RemoveAll(filepath.Dir(s3ResolutionContentPath(fixture.resolver, fixture.first))); err != nil {
				t.Fatal(err)
			}
			return nil
		}},
		{name: "missing content", mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			if err := os.Remove(s3ResolutionContentPath(fixture.resolver, fixture.first)); err != nil {
				t.Fatal(err)
			}
			return nil
		}},
		{name: "poisoned bytes", mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			if err := os.WriteFile(s3ResolutionContentPath(fixture.resolver, fixture.first), []byte("poisoned\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return nil
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newImmutableCorruptionFixture(t, t.TempDir(), "s3://daem/object", "v1", []byte("trusted\n"))
			verifyTarget := test.mutate(t, fixture)
			assertImmutableFallbackRepairs(t, fixture)
			if verifyTarget != nil {
				verifyTarget(t)
			}
		})
	}
}

func TestResolveDoesNotRetireUnownedOrRedirectedArtifactEntries(t *testing.T) {
	tests := []struct {
		name         string
		wantGetCalls int
		mutate       func(*testing.T, immutableCorruptionFixture) func(*testing.T)
	}{
		{name: "missing completion", wantGetCalls: 2, mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			contentPath := s3ResolutionContentPath(fixture.resolver, fixture.first)
			if err := os.Remove(filepath.Join(filepath.Dir(contentPath), immutableTestCompletionRecordName)); err != nil {
				t.Fatal(err)
			}
			return func(t *testing.T) { assertFileContent(t, contentPath, []byte("trusted\n")) }
		}},
		{name: "content symlink", wantGetCalls: 1, mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			contentPath := s3ResolutionContentPath(fixture.resolver, fixture.first)
			targetPath := filepath.Join(filepath.Dir(filepath.Dir(contentPath)), "outside-content")
			if err := os.WriteFile(targetPath, []byte("trusted\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(contentPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(targetPath, contentPath); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			return func(t *testing.T) { assertFileContent(t, targetPath, []byte("trusted\n")) }
		}},
		{name: "artifact root symlink", wantGetCalls: 1, mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			artifactRoot := filepath.Dir(s3ResolutionContentPath(fixture.resolver, fixture.first))
			targetRoot := artifactRoot + ".outside"
			if err := os.Rename(artifactRoot, targetRoot); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(targetRoot, artifactRoot); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			return func(t *testing.T) {
				assertFileContent(t, filepath.Join(targetRoot, "content"), []byte("trusted\n"))
			}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newImmutableCorruptionFixture(t, t.TempDir(), "s3://daem/object", "v1", []byte("trusted\n"))
			verifyTarget := test.mutate(t, fixture)
			if _, err := fixture.resolver.Resolve(t.Context(), fixture.sourceSpec, noOperationOptions); err == nil {
				t.Fatal("Resolve succeeded after cache ownership or authority became unprovable")
			}
			if calls := fixture.client.callCount(); calls != test.wantGetCalls {
				t.Fatalf("GetObject calls = %d, want %d", calls, test.wantGetCalls)
			}
			verifyTarget(t)
		})
	}
}

func TestResolveRejectsSwappedArtifactCompletionWithoutChangingOtherArtifact(t *testing.T) {
	cacheRoot := t.TempDir()
	client := &fakeS3Client{
		bodies:     [][]byte{[]byte("left\n"), []byte("right\n"), []byte("left\n")},
		versionIDs: []string{"v1", "v1", "v1"},
	}
	resolver, err := newResolverWithClient(cacheRoot, client)
	if err != nil {
		t.Fatal(err)
	}
	leftSource := sourcetest.S3(t, "s3://daem/left", "v1", "", sourcepkg.S3ObjectFormatFile)
	rightSource := sourcetest.S3(t, "s3://daem/right", "v1", "", sourcepkg.S3ObjectFormatFile)
	left := mustResolveS3(t, resolver, leftSource)
	right := mustResolveS3(t, resolver, rightSource)
	leftCompletion := filepath.Join(filepath.Dir(s3ResolutionContentPath(resolver, left)), immutableTestCompletionRecordName)
	rightCompletion := filepath.Join(filepath.Dir(s3ResolutionContentPath(resolver, right)), immutableTestCompletionRecordName)
	rightRecord := mustReadFile(t, rightCompletion)
	if err := os.WriteFile(leftCompletion, rightRecord, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := resolver.Resolve(t.Context(), leftSource, noOperationOptions); err == nil {
		t.Fatal("Resolve succeeded with a completion record owned by another cache key")
	}
	assertFileContent(t, leftCompletion, rightRecord)
	assertFileContent(t, rightCompletion, rightRecord)
	if resolved := mustResolveS3(t, resolver, rightSource); resolved != right {
		t.Fatalf("unrelated right artifact = %#v, want %#v", resolved, right)
	}
	if calls := client.callCount(); calls != 3 {
		t.Fatalf("GetObject calls = %d, want one blocked repair attempt and one unrelated hit", calls)
	}
}
