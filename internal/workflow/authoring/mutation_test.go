package authoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/operationplan"
)

func TestAuthoringDomainsSerializeReadReferentAndReplacedEntry(t *testing.T) {
	root := t.TempDir()
	store, err := mutation.NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	currentLockfile := filepath.Join(root, "current.lock.toml")
	referent := filepath.Join(root, "referent.lock.toml")
	if err := os.WriteFile(referent, []byte("lock"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, currentLockfile); err != nil {
		t.Fatal(err)
	}
	currentDomain, err := mutation.NewLogicalPathDomain(mutation.LogicalPathRequest{
		Path: referent, Access: mutation.AccessExclusive, Effect: mutation.PathEffectReferent,
	})
	if err != nil {
		t.Fatal(err)
	}
	holder, err := store.Acquire(context.Background(), currentDomain)
	if err != nil {
		t.Fatal(err)
	}

	program := operationplan.CompileAuthoring(operationplan.AuthoringInput{
		ManifestPath:         filepath.Join(root, "daem.toml"),
		LockfilePath:         currentLockfile,
		MarkerPath:           filepath.Join(root, ".daem", "metadata-transaction"),
		DocumentMaximumBytes: 1024,
	})
	domains, err := lowerAuthoringDomainSteps(program.DomainSteps())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = store.Acquire(ctx, domains...)
	var canceled mutation.CancellationError
	if !errors.As(err, &canceled) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire error = %v, want referent cancellation", err)
	}
	if err := holder.Release(); err != nil {
		t.Fatal(err)
	}
	set, err := store.Acquire(context.Background(), domains...)
	if err != nil {
		t.Fatalf("Acquire after holder release: %v", err)
	}
	if err := set.Release(); err != nil {
		t.Fatal(err)
	}
}
