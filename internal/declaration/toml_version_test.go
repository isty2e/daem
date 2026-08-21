package declaration

import (
	"fmt"
	"math"
	"testing"
)

var (
	_ int64 = CurrentManifestVersion
	_ int64 = Manifest{}.Version
)

func TestDecodeManifestPreservesWidthIndependentVersion(t *testing.T) {
	for _, want := range []int64{1<<32 + 1, math.MaxInt64} {
		manifest, err := DecodeManifest(fmt.Appendf(nil, "version = %d\ntargets = []\n", want))
		if err != nil {
			t.Fatalf("DecodeManifest(%d) returned error: %v", want, err)
		}
		if manifest.Version != want {
			t.Fatalf("DecodeManifest(%d) version = %d", want, manifest.Version)
		}
	}
}

func TestDecodeManifestRejectsIntegerOutsideWireDomain(t *testing.T) {
	if _, err := DecodeManifest([]byte("version = 9223372036854775808\ntargets = []\n")); err == nil {
		t.Fatal("DecodeManifest accepted a version above int64")
	}
}
