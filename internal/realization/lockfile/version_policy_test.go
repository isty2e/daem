package lockfile

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/lock"
)

var (
	_ func(context.Context, []byte) (int64, error) = lockfileVersion
	_ int64                                        = fileDTO{}.Version
	_ int64                                        = repairRecipeDTO{}.Version
	_ int64                                        = versionEnvelopeDTO{}.Version
)

func TestUnsupportedVersionRelockPolicy(t *testing.T) {
	tests := []struct {
		version int64
		want    bool
	}{
		{version: 2, want: false},
		{version: 3, want: true},
		{version: 4, want: true},
		{version: 5, want: true},
		{version: lock.CurrentVersion, want: false},
		{version: lock.CurrentVersion + 1, want: false},
		{version: 1<<32 + 3, want: false},
		{version: 1<<32 + 4, want: false},
		{version: 1<<32 + 5, want: false},
		{version: 1<<32 + 6, want: false},
	}

	for _, test := range tests {
		err := UnsupportedVersionError{Found: test.version, Supported: lock.CurrentVersion}
		if got := err.RelockSupported(); got != test.want {
			t.Fatalf("version %d RelockSupported() = %t, want %t", test.version, got, test.want)
		}
	}
}

func TestLockfileVersionPreservesWidthIndependentWireValues(t *testing.T) {
	versions := []int64{1<<32 + 3, 1<<32 + 4, 1<<32 + 5, 1<<32 + 6, math.MaxInt64}
	for _, want := range versions {
		got, err := lockfileVersion(t.Context(), fmt.Appendf(nil, "version = %d\n", want))
		if err != nil {
			t.Fatalf("lockfileVersion(%d) returned error: %v", want, err)
		}
		if got != want {
			t.Fatalf("lockfileVersion(%d) = %d", want, got)
		}
	}
}

func TestWidthCongruentFutureVersionsNeverAcquireLockfileAuthority(t *testing.T) {
	canonical := marshalLockfileForTest(t, lockfileWithSubjects(t))
	for offset := int64(3); offset <= int64(lock.CurrentVersion); offset++ {
		future := 1<<32 + offset
		content := []byte(replaceLockfileStringOnce(
			t,
			canonical,
			currentLockfileVersionEnvelope(),
			fmt.Sprintf("version = %d", future),
		))

		operations := []struct {
			name string
			run  func() error
		}{
			{name: "load", run: func() error { _, err := loadContent(t.Context(), content); return err }},
			{name: "replacement", run: func() error { return validateReplacementContent(t.Context(), content) }},
		}
		for _, operation := range operations {
			err := operation.run()
			var versionErr UnsupportedVersionError
			if !errors.As(err, &versionErr) {
				t.Fatalf("%s version %d error = %v, want UnsupportedVersionError", operation.name, future, err)
			}
			if versionErr.Found != future || versionErr.RelockSupported() {
				t.Fatalf("%s version %d error = %#v", operation.name, future, versionErr)
			}
			for _, want := range []string{fmt.Sprintf("unsupported lockfile version %d", future), "newer daem"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("%s version %d error = %q, want %q", operation.name, future, err, want)
				}
			}
		}
	}
}

func TestLockfileVersionRejectsIntegerOutsideWireDomain(t *testing.T) {
	_, err := lockfileVersion(t.Context(), []byte("version = 9223372036854775808\n"))
	if err == nil {
		t.Fatal("lockfileVersion accepted an integer above int64")
	}
}

func TestSnapshotFromDTORequiresExactCurrentWireVersion(t *testing.T) {
	future := int64(lock.CurrentVersion + 1)
	_, err := snapshotFromDTO(fileDTO{Version: future})
	var versionErr UnsupportedVersionError
	if !errors.As(err, &versionErr) || versionErr.Found != future {
		t.Fatalf("snapshotFromDTO error = %v, want UnsupportedVersionError(%d)", err, future)
	}
}

func TestValidateReplacementContentTreatsLegacyBodiesAsOpaqueAndFutureBodiesAsAuthoritative(t *testing.T) {
	if err := validateReplacementContent(t.Context(), []byte("version = 3\nfuture_or_legacy_payload = { malformed = [\n")); err != nil {
		t.Fatalf("schema 3 opaque replacement authority returned error: %v", err)
	}

	future := []byte("version = 7\nfuture_payload = { malformed = [\n")
	err := validateReplacementContent(t.Context(), future)
	if err == nil {
		t.Fatal("future lockfile replacement returned nil error")
	}
	for _, want := range []string{"unsupported lockfile version 7", "newer daem"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("future lockfile error = %q, want %q", err, want)
		}
	}
}
