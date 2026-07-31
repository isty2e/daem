package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoreRejectsSelfOverlapAndUnsafeLockRecord(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	overlap := mutationTestLogicalDomain(t, dataDir, AccessExclusive)
	if _, err := store.Acquire(context.Background(), overlap); err == nil || !strings.Contains(err.Error(), "contains the lease store") {
		t.Fatalf("self-overlap error = %v", err)
	}

	safe := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "safe"), AccessExclusive)
	normalized, err := store.normalize([]Domain{safe})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.prepare(); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(store.root, lockRecordName(normalized[0].key))
	if err := os.Symlink(filepath.Join(dataDir, "elsewhere"), lockPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := store.Acquire(context.Background(), safe); err == nil || !strings.Contains(err.Error(), "unsupported file mode") {
		t.Fatalf("unsafe lock-record error = %v", err)
	}
}

func TestLeaseReleaseIsIdempotentAndRecordsRemain(t *testing.T) {
	store := mutationTestStore(t)
	domain := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "resource"), AccessExclusive)
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Release(); err != nil {
		t.Fatal(err)
	}
	if err := set.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := set.DomainsMatchCurrent(context.Background()); err == nil {
		t.Fatal("released lease set still reported current domain authority")
	}
	entries, err := os.ReadDir(store.root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("release unlinked every lock record")
	}
}

func TestIndependentStoresSerializeSameProcessExclusiveLeases(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "resource")
	firstStore := mutationTestStoreAt(t, dataDir)
	first, err := firstStore.Acquire(
		context.Background(),
		mutationTestLogicalDomain(t, path, AccessExclusive),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	secondStore := mutationTestStoreAt(t, dataDir)
	secondStore.maximum = 50 * time.Millisecond
	_, err = secondStore.Acquire(
		context.Background(),
		mutationTestLogicalDomain(t, path, AccessExclusive),
	)
	var contention ContentionError
	if !errors.As(err, &contention) {
		t.Fatalf("same-process exclusive contention error = %v", err)
	}
}

func TestIndependentStoresShareSameProcessSharedLeaseUntilLastRelease(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "resource")
	firstStore := mutationTestStoreAt(t, dataDir)
	first, err := firstStore.Acquire(
		context.Background(),
		mutationTestLogicalDomain(t, path, AccessShared),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()

	secondStore := mutationTestStoreAt(t, dataDir)
	second, err := secondStore.Acquire(
		context.Background(),
		mutationTestLogicalDomain(t, path, AccessShared),
	)
	if err != nil {
		t.Fatalf("same-process shared acquisition error: %v", err)
	}
	defer second.Release()

	probe := mutationTestStoreAt(t, dataDir)
	probe.maximum = 50 * time.Millisecond
	exclusive := mutationTestLogicalDomain(t, path, AccessExclusive)
	if _, err := probe.Acquire(context.Background(), exclusive); err == nil {
		t.Fatal("exclusive lease bypassed two same-process shared holders")
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := probe.Acquire(context.Background(), exclusive); err == nil {
		t.Fatal("exclusive lease bypassed remaining same-process shared holder")
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	acquired, err := probe.Acquire(context.Background(), exclusive)
	if err != nil {
		t.Fatalf("exclusive acquisition after final shared release: %v", err)
	}
	if err := acquired.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentStoresSerializeIdenticalNestedDomainSets(t *testing.T) {
	dataDir := t.TempDir()
	recoveryDir := filepath.Join(t.TempDir(), "recovery")
	paths := []string{
		recoveryDir,
		filepath.Join(recoveryDir, "control"),
		filepath.Join(recoveryDir, "control", "record.json"),
		filepath.Join(recoveryDir, "residue"),
		filepath.Join(recoveryDir, "garbage"),
	}
	domains := make([]Domain, 0, len(paths)*2)
	for _, path := range paths {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			domain, err := NewLogicalPathDomain(LogicalPathRequest{
				Path:   path,
				Access: AccessExclusive,
				Effect: effect,
			})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}

	firstStore := mutationTestStoreAt(t, dataDir)
	first, err := firstStore.Acquire(context.Background(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Release()
	secondStore := mutationTestStoreAt(t, dataDir)
	secondStore.maximum = 50 * time.Millisecond
	_, err = secondStore.Acquire(context.Background(), domains...)
	var contention ContentionError
	if !errors.As(err, &contention) {
		t.Fatalf("same-process nested-domain contention error = %v", err)
	}
}

func TestStoreDoesNotBroadenExistingDataDirectoryPermissions(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.Chmod(dataDir, 0o750); err != nil {
		t.Fatal(err)
	}
	store := mutationTestStoreAt(t, dataDir)
	domain := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "resource"), AccessExclusive)
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	_ = set.Release()
	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o750 {
		t.Fatalf("data directory permissions = %o, want 750", info.Mode().Perm())
	}
}

func TestStoreRejectsWritableDataRootAndCreationAnchor(t *testing.T) {
	t.Run("existing data root", func(t *testing.T) {
		dataDir := t.TempDir()
		if err := os.Chmod(dataDir, 0o770); err != nil {
			t.Fatal(err)
		}
		store := mutationTestStoreAt(t, dataDir)
		domain := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "resource"), AccessExclusive)
		if _, err := store.Acquire(context.Background(), domain); err == nil ||
			!strings.Contains(err.Error(), "group/world-writable") {
			t.Fatalf("Acquire error = %v, want writable data-root rejection", err)
		}
	})

	t.Run("missing data root below writable parent", func(t *testing.T) {
		parent := t.TempDir()
		if err := os.Chmod(parent, 0o777); err != nil {
			t.Fatal(err)
		}
		store := mutationTestStoreAt(t, filepath.Join(parent, "daem"))
		domain := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "resource"), AccessExclusive)
		if _, err := store.Acquire(context.Background(), domain); err == nil ||
			!strings.Contains(err.Error(), "group/world-writable") {
			t.Fatalf("Acquire error = %v, want writable creation-anchor rejection", err)
		}
	})
}

func TestStoreRejectsExposedExistingLockRecord(t *testing.T) {
	store := mutationTestStore(t)
	domain := mutationTestLogicalDomain(t, filepath.Join(t.TempDir(), "resource"), AccessExclusive)
	normalized, err := store.normalize([]Domain{domain})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.prepare(); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(store.root, lockRecordName(normalized[0].key))
	if err := os.WriteFile(lockPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(context.Background(), domain); err == nil ||
		!strings.Contains(err.Error(), "not private") {
		t.Fatalf("Acquire error = %v, want exposed lock-record rejection", err)
	}
}

func TestStoreDataDirReturnsSelectedPhysicalReferent(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Skipf("create data-root alias: %v", err)
	}
	store, err := NewStore(alias)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(physical)
	if err != nil {
		t.Fatal(err)
	}
	if store.DataDir() != want {
		t.Fatalf("Store.DataDir = %q, want %q", store.DataDir(), want)
	}
}

func TestStoreCreatesMissingNestedDataDirectoryWithoutChangingExistingParent(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(parent, ".local", "share", "daem")
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	domain, err := NewLogicalPathDomain(LogicalPathRequest{
		Path:   filepath.Join(parent, "manifest.toml"),
		Access: AccessExclusive,
		Effect: PathEffectDirectoryEntry,
	})
	if err != nil {
		t.Fatal(err)
	}
	set, err := store.Acquire(context.Background(), domain)
	if err != nil {
		t.Fatal(err)
	}
	if err := set.Release(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("existing parent mode = %o, want 755", got)
	}
	for _, path := range []string{
		filepath.Join(parent, ".local"),
		filepath.Join(parent, ".local", "share"),
		dataDir,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("created directory %q mode = %o, want 700", path, got)
		}
	}
}
