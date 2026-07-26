//go:build darwin || linux

package access

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMaterializationReleaseIsConcurrentIdempotentAndErrorBearing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	view := accessTestView(t, root)
	releaseFailure := errors.New("remove temporary artifact")
	var releases atomic.Int32
	materialization, err := NewMaterialization(view, func() error {
		releases.Add(1)
		return releaseFailure
	})
	if err != nil {
		t.Fatalf("NewMaterialization returned error: %v", err)
	}

	const callers = 8
	errorsByCaller := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Go(func() {
			errorsByCaller <- materialization.Release()
		})
	}
	wait.Wait()
	close(errorsByCaller)
	for releaseErr := range errorsByCaller {
		if !errors.Is(releaseErr, releaseFailure) {
			t.Fatalf("Release error = %v, want stable cleanup failure", releaseErr)
		}
	}
	if releases.Load() != 1 {
		t.Fatalf("release callback calls = %d, want 1", releases.Load())
	}
	if _, err := materialization.View(); err == nil {
		t.Fatal("View after Release returned nil error")
	}
}

func TestMaterializationDoesNotGrantViewLivenessAfterRelease(t *testing.T) {
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	materialization, err := NewMaterialization(accessTestView(t, root), func() error {
		return os.Remove(root)
	})
	if err != nil {
		t.Fatalf("NewMaterialization returned error: %v", err)
	}
	heldView, err := materialization.View()
	if err != nil {
		t.Fatalf("View returned error: %v", err)
	}
	if err := materialization.Release(); err != nil {
		t.Fatalf("Release returned error: %v", err)
	}
	if _, err := heldView.Hash(context.Background()); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("held View Hash error = %v, want not-exist after release", err)
	}
}

func TestMaterializationReleaseCallbackCanObserveReleasedState(t *testing.T) {
	root := t.TempDir()
	view := accessTestView(t, root)
	var materialization *Materialization
	constructed, err := NewMaterialization(view, func() error {
		_, viewErr := materialization.View()
		if viewErr == nil || !strings.Contains(viewErr.Error(), "has been released") {
			return fmt.Errorf("View during release error = %v, want released state", viewErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("NewMaterialization returned error: %v", err)
	}
	materialization = constructed

	done := make(chan error, 1)
	go func() { done <- materialization.Release() }()
	select {
	case releaseErr := <-done:
		if releaseErr != nil {
			t.Fatalf("Release returned error: %v", releaseErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Release deadlocked while callback observed released state")
	}
}

func TestMaterializationRejectsInvalidConstructionAndNilReceiver(t *testing.T) {
	if _, err := NewMaterialization(View{}, func() error { return nil }); err == nil {
		t.Fatal("NewMaterialization accepted zero View")
	}
	root := filepath.Join(t.TempDir(), "artifact")
	writeAccessTestFile(t, root, []byte("content\n"))
	if _, err := NewMaterialization(accessTestView(t, root), nil); err == nil {
		t.Fatal("NewMaterialization accepted nil release callback")
	}

	var materialization *Materialization
	if _, err := materialization.View(); err == nil {
		t.Fatal("nil Materialization.View returned nil error")
	}
	if err := materialization.Release(); err == nil {
		t.Fatal("nil Materialization.Release returned nil error")
	}
}
