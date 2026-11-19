package lockfile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	tomlsyntax "github.com/isty2e/daem/internal/encoding/toml"
	"github.com/isty2e/daem/internal/realization/lock"
)

func TestDecodeForRelockDistinguishesDamagedTOMLFromUnreadableLock(t *testing.T) {
	version := fmt.Sprintf("version = %d\n", lock.CurrentVersion)
	deepArray := strings.Repeat("[", tomlsyntax.MaximumDepth+1) + "0" + strings.Repeat("]", tomlsyntax.MaximumDepth+1)
	for _, test := range []struct {
		name    string
		content string
		want    error
	}{
		{"incomplete table", version + "[[\n", ErrDamagedCurrent},
		{"duplicate key in table", version + "[locked]\nkey = 1\nkey = 2\n", ErrDamagedCurrent},
		{"invalid value in unmapped field", version + "unknown = 2026-99-99\n", ErrDamagedCurrent},
		{"valid unknown field", version + "unknown = 2026-11-18\n", nil},
		{"wrong field type", version + "locked = \"not a table\"\n", nil},
		{"conflict at envelope", "<<<<<<< ours\n" + version + "=======\nversion = 8\n>>>>>>> theirs\n", nil},
		{"invalid UTF-8", version + "\xff", nil},
		{"depth limit before malformed tail", version + "payload = " + deepArray + "\n[[\n", tomlsyntax.ErrMaximumDepthExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			content := []byte(test.content)
			_, err := DecodeForRelock(t.Context(), content)
			if err == nil || (test.want != nil && !errors.Is(err, test.want)) ||
				errors.Is(err, ErrDamagedCurrent) != (test.want == ErrDamagedCurrent) {
				t.Fatalf("relock error = %v, want refusal classified as %v", err, test.want)
			}
			if test.want == ErrDamagedCurrent {
				if err := validateReplacementContent(t.Context(), content); err == nil || errors.Is(err, ErrDamagedCurrent) {
					t.Fatalf("paired authoring changed its admission: %v", err)
				}
			}
		})
	}
}

func TestRelockWireProbePreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	content := fmt.Appendf(nil, "version = %d\nunknown = 2026-99-99\n", lock.CurrentVersion)
	wireErr := errors.New("unknown field")
	err := classifyLockfileWireFailure(ctx, content, wireErr)
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrDamagedCurrent) {
		t.Fatalf("cancelled syntax probe = %v", err)
	}
}
