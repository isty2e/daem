package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCrossProcessSharedAndExclusiveContention(t *testing.T) {
	dataDir := t.TempDir()
	path := filepath.Join(t.TempDir(), "resource")
	sharedHolder := startMutationLeaseHelper(t, dataDir, "shared", 5*time.Second, path)

	store := mutationTestStoreAt(t, dataDir)
	store.maximum = 150 * time.Millisecond
	shared := mutationTestLogicalDomain(t, path, AccessShared)
	sharedSet, err := store.Acquire(context.Background(), shared)
	if err != nil {
		t.Fatalf("shared/shared acquisition error: %v", err)
	}
	if err := sharedSet.Release(); err != nil {
		t.Fatal(err)
	}
	exclusive := mutationTestLogicalDomain(t, path, AccessExclusive)
	_, err = store.Acquire(context.Background(), exclusive)
	var contention ContentionError
	if !errors.As(err, &contention) {
		t.Fatalf("shared/exclusive error = %v", err)
	}
	stopMutationLeaseHelper(t, sharedHolder)

	exclusiveHolder := startMutationLeaseHelper(t, dataDir, "exclusive", 5*time.Second, path)
	_, err = store.Acquire(context.Background(), exclusive)
	if !errors.As(err, &contention) {
		t.Fatalf("exclusive/exclusive error = %v", err)
	}
	stopMutationLeaseHelper(t, exclusiveHolder)
}

func TestCrossProcessAliasContentionAndHolderDeathRelease(t *testing.T) {
	dataDir := t.TempDir()
	root := t.TempDir()
	realParent := filepath.Join(root, "real")
	if err := mkdirMutationTest(realParent); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "alias")
	if err := os.Symlink(realParent, aliasParent); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	realPath := filepath.Join(realParent, "resource")
	aliasPath := filepath.Join(aliasParent, "resource")
	holder := startMutationLeaseHelper(t, dataDir, "exclusive", 10*time.Second, realPath)

	store := mutationTestStoreAt(t, dataDir)
	store.maximum = 150 * time.Millisecond
	aliasDomain := mutationTestLogicalDomain(t, aliasPath, AccessExclusive)
	_, err := store.Acquire(context.Background(), aliasDomain)
	var contention ContentionError
	if !errors.As(err, &contention) {
		t.Fatalf("alias contention error = %v", err)
	}
	stopMutationLeaseHelper(t, holder)

	set, err := store.Acquire(context.Background(), aliasDomain)
	if err != nil {
		t.Fatalf("acquire after holder death error: %v", err)
	}
	if err := set.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledWaitReleasesAcquiredPrefix(t *testing.T) {
	dataDir := t.TempDir()
	root := t.TempDir()
	firstPath := filepath.Join(root, "a")
	blockedPath := filepath.Join(root, "z")
	holder := startMutationLeaseHelper(t, dataDir, "exclusive", 5*time.Second, blockedPath)
	defer stopMutationLeaseHelper(t, holder)

	store := mutationTestStoreAt(t, dataDir)
	store.maximum = 3 * time.Second
	first := mutationTestLogicalDomain(t, firstPath, AccessExclusive)
	blocked := mutationTestLogicalDomain(t, blockedPath, AccessExclusive)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := store.Acquire(ctx, blocked, first)
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	err := <-done
	var cancellation CancellationError
	if !errors.As(err, &cancellation) || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled acquisition error = %v", err)
	}

	probe := mutationTestStoreAt(t, dataDir)
	probe.maximum = 200 * time.Millisecond
	set, err := probe.Acquire(context.Background(), first)
	if err != nil {
		t.Fatalf("acquired prefix was not released: %v", err)
	}
	_ = set.Release()
}

func TestReversedCrossProcessSetsDoNotDeadlock(t *testing.T) {
	dataDir := t.TempDir()
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	left := startMutationLeaseHelper(t, dataDir, "exclusive", 100*time.Millisecond, first, second)
	right := startMutationLeaseHelper(t, dataDir, "exclusive", 100*time.Millisecond, second, first)
	waitMutationLeaseHelper(t, left, 3*time.Second)
	waitMutationLeaseHelper(t, right, 3*time.Second)
}

func TestMutationLeaseHelperProcess(t *testing.T) {
	if os.Getenv("DAEM_MUTATION_LEASE_HELPER") != "1" {
		return
	}
	args := argsAfterMutationTestSeparator(os.Args)
	if len(args) < 5 {
		os.Exit(90)
	}
	dataDir := args[0]
	readyPath := args[1]
	access := AccessExclusive
	if args[2] == "shared" {
		access = AccessShared
	}
	hold, err := time.ParseDuration(args[3])
	if err != nil {
		os.Exit(91)
	}
	store, err := NewStore(dataDir)
	if err != nil {
		os.Exit(92)
	}
	domains := make([]Domain, 0, len(args)-4)
	for _, path := range args[4:] {
		domain, err := NewLogicalPathDomain(LogicalPathRequest{Path: path, Access: access, Effect: PathEffectDirectoryEntry})
		if err != nil {
			os.Exit(93)
		}
		domains = append(domains, domain)
	}
	set, err := store.Acquire(context.Background(), domains...)
	if err != nil {
		os.Exit(94)
	}
	if err := os.WriteFile(readyPath, []byte("ready"), 0o600); err != nil {
		os.Exit(95)
	}
	time.Sleep(hold)
	if err := set.Release(); err != nil {
		os.Exit(96)
	}
}
