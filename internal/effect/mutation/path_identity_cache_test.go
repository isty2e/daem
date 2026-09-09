package mutation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedPathIdentitiesKeepKeysAndRetryFailures(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	calls := 0
	cached := cachePathIdentities(func(path string, effect PathEffect) (canonicalPath, error) {
		calls++
		return canonicalPathIdentity(path, effect)
	})
	for pass := 0; pass < 3; pass++ {
		for _, path := range []string{link, target} {
			for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
				want, err := canonicalPathIdentity(path, effect)
				if err != nil {
					t.Fatal(err)
				}
				got, err := cached(path, effect)
				if err != nil || got != want {
					t.Fatalf("path=%s effect=%v: got=%#v,%v want=%#v", path, effect, got, err, want)
				}
			}
		}
	}
	if calls != 4 {
		t.Fatalf("complete observations=%d, want 4", calls)
	}

	unavailable := errors.New("observation unavailable")
	attempts := 0
	retry := cachePathIdentities(func(path string, effect PathEffect) (canonicalPath, error) {
		attempts++
		if attempts == 1 {
			return canonicalPath{}, unavailable
		}
		return canonicalPathIdentity(path, effect)
	})
	if got, err := retry(target, PathEffectReferent); got != (canonicalPath{}) || !errors.Is(err, unavailable) {
		t.Fatalf("first observation=%#v,%v", got, err)
	}
	want, err := canonicalPathIdentity(target, PathEffectReferent)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if got, err := retry(target, PathEffectReferent); err != nil || got != want {
			t.Fatalf("retry observation=%#v,%v want=%#v", got, err, want)
		}
	}
	if attempts != 2 {
		t.Fatalf("observation attempts=%d, want 2", attempts)
	}
}
