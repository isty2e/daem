package manifest

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestDecodeRejectsWidthIndependentFutureManifestVersions(t *testing.T) {
	for _, version := range []int64{1<<32 + 1, math.MaxInt64} {
		_, err := Decode(fmt.Appendf(nil, "version = %d\ntargets = [\"codex\"]\n", version))
		want := fmt.Sprintf("unsupported manifest version %d", version)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Decode(%d) error = %v, want %q", version, err, want)
		}
	}
}
