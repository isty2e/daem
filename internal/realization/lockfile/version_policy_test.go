package lockfile

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lock"
)

func TestUnsupportedVersionRelockPolicy(t *testing.T) {
	tests := []struct {
		version int
		want    bool
	}{
		{version: 2, want: false},
		{version: 3, want: true},
		{version: 4, want: true},
		{version: 5, want: true},
		{version: lock.CurrentVersion, want: false},
		{version: lock.CurrentVersion + 1, want: false},
	}

	for _, test := range tests {
		err := UnsupportedVersionError{Found: test.version, Supported: lock.CurrentVersion}
		if got := err.RelockSupported(); got != test.want {
			t.Fatalf("version %d RelockSupported() = %t, want %t", test.version, got, test.want)
		}
	}
}

func TestValidateReplacementContentTreatsLegacyBodiesAsOpaqueAndFutureBodiesAsAuthoritative(t *testing.T) {
	if err := ValidateReplacementContent([]byte("version = 3\nfuture_or_legacy_payload = { malformed = [\n")); err != nil {
		t.Fatalf("schema 3 opaque replacement authority returned error: %v", err)
	}

	future := []byte("version = 7\nfuture_payload = { malformed = [\n")
	err := ValidateReplacementContent(future)
	if err == nil {
		t.Fatal("future lockfile replacement returned nil error")
	}
	for _, want := range []string{"unsupported lockfile version 7", "newer daem"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("future lockfile error = %q, want %q", err, want)
		}
	}
}
