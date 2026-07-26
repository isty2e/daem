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
		{name: "missing completion", mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			if err := os.Remove(filepath.Join(filepath.Dir(s3ResolutionContentPath(fixture.resolver, fixture.first)), immutableTestCompletionRecordName)); err != nil {
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
		{name: "content symlink", mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
			targetPath := filepath.Join(filepath.Dir(filepath.Dir(s3ResolutionContentPath(fixture.resolver, fixture.first))), "outside-content")
			if err := os.WriteFile(targetPath, []byte("trusted\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(s3ResolutionContentPath(fixture.resolver, fixture.first)); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(targetPath, s3ResolutionContentPath(fixture.resolver, fixture.first)); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			return func(t *testing.T) { assertFileContent(t, targetPath, []byte("trusted\n")) }
		}},
		{name: "artifact root symlink", mutate: func(t *testing.T, fixture immutableCorruptionFixture) func(*testing.T) {
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
			assertImmutableFallbackRepairs(t, fixture)
			if verifyTarget != nil {
				verifyTarget(t)
			}
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

	if resolved := mustResolveS3(t, resolver, leftSource); resolved != left {
		t.Fatalf("repaired left artifact = %#v, want %#v", resolved, left)
	}
	assertFileContent(t, rightCompletion, rightRecord)
	if resolved := mustResolveS3(t, resolver, rightSource); resolved != right {
		t.Fatalf("unrelated right artifact = %#v, want %#v", resolved, right)
	}
	if calls := client.callCount(); calls != 3 {
		t.Fatalf("GetObject calls = %d, want one repair and one unrelated hit", calls)
	}
}
