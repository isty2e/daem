package codexplugin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/filesnapshot"
)

func TestConfigReaderRetainsPresentAndAbsentEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("malformed = ["), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, exists := range []bool{false, true} {
		calls := 0
		observation, err := ObserveConfigFileWithReader(t.Context(), path, func(ctx context.Context, requested string, maximum int64) ([]byte, bool, error) {
			calls++
			if requested != path || maximum != MaximumConfigBytes || ctx.Err() != nil {
				t.Fatal("reader lost selected ingress facts")
			}
			if !exists {
				return nil, false, nil
			}
			return []byte("[plugins.\"documents@official\"]\nenabled=true\n"), true, nil
		})
		if err != nil || calls != 1 || observation.ConfigExists() != exists {
			t.Fatal("retained evidence was reread or discarded", err)
		}
		if exists && (!observation.EntrySetObserved() || len(observation.Entries()) != 1 || string(observation.Entries()[0].Key()) != "documents@official") {
			t.Fatal("retained bytes lost canonical entry decoding")
		}
	}
}

func TestConfigReaderRejectsErrorsOversizeAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	cause := errors.New("unavailable retained document")
	if _, err := ObserveConfigFileWithReader(t.Context(), path, func(context.Context, string, int64) ([]byte, bool, error) { return nil, false, cause }); !errors.Is(err, cause) {
		t.Fatal("unavailable input became fresh absence", err)
	}
	if _, err := ObserveConfigFileWithReader(t.Context(), path, func(context.Context, string, int64) ([]byte, bool, error) {
		return make([]byte, MaximumConfigBytes+1), true, nil
	}); !errors.Is(err, filesnapshot.ErrLimitExceeded) {
		t.Fatal("reader bypassed Codex's observation limit", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if _, err := ObserveConfigFileWithReader(ctx, path, func(context.Context, string, int64) ([]byte, bool, error) { called = true; return nil, false, nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled observation invoked a reader", err)
	}
}
