//go:build darwin

package platform

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/subprocess"
)

func TestCurrentRecordsNativeMacOSProductVersion(t *testing.T) {
	observation, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	version, observed := observation.Version()
	if !observed {
		t.Fatalf("native product version was not observed: reason=%s", observation.Reason())
	}
	t.Logf("observed macOS product version %s", version)
}

func TestCurrentParsesProductVersionWhenAmbientSecretMatchesOutput(t *testing.T) {
	first, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	version, observed := first.Version()
	if !observed {
		t.Fatalf("native product version was not observed: reason=%s", first.Reason())
	}
	t.Setenv("PR66_API_KEY", version.String())
	observation, err := Current(context.Background())
	if err != nil {
		t.Fatalf("Current with matching ambient secret: %v", err)
	}
	got, observed := observation.Version()
	if !observed || got.String() != version.String() {
		t.Fatalf("version = %s,%t, want %s after ambient secret matched output", got, observed, version)
	}
}

func TestJoinDarwinCommandCleanupPreservesFrozenTimeout(t *testing.T) {
	result := commandResult{timedOut: true, err: context.DeadlineExceeded}
	got := joinDarwinCommandCleanup(result, subprocess.ProcessTermination{}, errors.New("cleanup"))
	if !got.timedOut || got.canceled {
		t.Fatalf("cleanup join = %#v, want frozen timeout", got)
	}
	if !errors.Is(got.err, context.DeadlineExceeded) {
		t.Fatalf("cleanup join error = %v, want frozen deadline", got.err)
	}
	if !strings.Contains(got.err.Error(), "cleanup") {
		t.Fatalf("cleanup join error = %v, want cleanup joined without rewriting timeout", got.err)
	}
}
