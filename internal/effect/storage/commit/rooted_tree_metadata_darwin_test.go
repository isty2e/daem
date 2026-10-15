//go:build darwin

package commit

import (
	"crypto/sha256"
	"errors"
	"slices"
	"testing"
)

func TestPreparedTreeCapturesOpaqueDarwinCreationXattrs(t *testing.T) {
	for _, test := range []struct {
		name   string
		values map[string][]byte
		names  []string
	}{
		{
			name:   "empty MACL remains present",
			values: map[string][]byte{"com.apple.macl": nil},
			names:  []string{"com.apple.macl"},
		},
		{
			name: "opaque MACL and provenance",
			values: map[string][]byte{
				"com.apple.macl":       {0, 0xff, 0x17},
				"com.apple.provenance": []byte("provenance"),
			},
			names: []string{"com.apple.provenance", "com.apple.macl"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reads []string
			facts, err := capturePreparedTreeXattrFacts("prepared", test.names, func(name string) ([]byte, error) {
				reads = append(reads, name)
				return test.values[name], nil
			})
			if err != nil {
				t.Fatalf("capture readable creation metadata: %v", err)
			}
			if len(facts) != len(test.values) || len(reads) != len(test.values) {
				t.Fatalf("captured facts=%v reads=%v, want every observed attribute", facts, reads)
			}

			if !slices.IsSorted(reads) {
				t.Fatalf("capture order is not canonical: %v", reads)
			}
			for _, fact := range facts {
				value, exists := test.values[fact.name]
				if !exists || fact.size != len(value) || fact.digest != sha256.Sum256(value) {
					t.Fatalf("attribute %q did not retain its opaque value fingerprint: %v", fact.name, fact)
				}
			}
		})
	}
}

func TestPreparedTreeMACLChangesRemainVisible(t *testing.T) {
	withMACL := map[string][]byte{
		"com.apple.macl":       []byte("one"),
		"com.apple.provenance": []byte("provenance"),
	}
	withoutMACL := map[string][]byte{"com.apple.provenance": []byte("provenance")}
	capture := func(t *testing.T, values map[string][]byte) preparedTreeMetadataFacts {
		t.Helper()

		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		facts, err := capturePreparedTreeXattrFacts("prepared", names, func(name string) ([]byte, error) {
			return values[name], nil
		})
		if err != nil {
			t.Fatalf("capture creation metadata: %v", err)
		}
		return preparedTreeMetadataFacts{xattrs: facts}
	}

	for _, test := range []struct {
		name      string
		before    map[string][]byte
		after     map[string][]byte
		wantEqual bool
	}{
		{name: "unchanged", before: withMACL, after: withMACL, wantEqual: true},
		{
			name: "same-size value change", before: withMACL,
			after: map[string][]byte{
				"com.apple.macl":       []byte("two"),
				"com.apple.provenance": []byte("provenance"),
			},
		},
		{name: "removed", before: withMACL, after: withoutMACL},
		{name: "added", before: withoutMACL, after: withMACL},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := capture(t, test.before)
			after := capture(t, test.after)
			if before.equal(after) != test.wantEqual {
				t.Fatalf("snapshot metadata comparison did not preserve %s", test.name)
			}
			if before.creationMetadata().equal(after.creationMetadata()) != test.wantEqual {
				t.Fatalf("creation metadata comparison did not preserve %s", test.name)
			}
		})
	}
}

func TestPreparedTreeXattrCaptureRefusesUnknownOrUnreadable(t *testing.T) {
	readFailure := errors.New("controlled attribute read failure")
	for _, test := range []struct {
		name      string
		names     []string
		failRead  string
		wantError error
	}{
		{
			name:  "unknown after admitted value",
			names: []string{"com.apple.macl", "com.apple.macl.extra"},
		},
		{
			name: "read failure after admitted value", names: []string{"com.apple.provenance", "com.apple.macl"},
			failRead: "com.apple.provenance", wantError: readFailure,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reads []string
			facts, err := capturePreparedTreeXattrFacts("prepared", test.names, func(name string) ([]byte, error) {
				reads = append(reads, name)
				if name == test.failRead {
					return nil, readFailure
				}
				return []byte("observed"), nil
			})
			if facts != nil || err == nil {
				t.Fatalf("failed capture returned usable partial evidence: facts=%v err=%v", facts, err)
			}
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("capture error = %v, want original read failure", err)
			}
			if test.wantError == nil && (!isUnsupported(err) || slices.Contains(reads, "com.apple.macl.extra")) {
				t.Fatalf("unknown attribute was not refused before reading: reads=%v err=%v", reads, err)
			}
		})
	}
}

func TestPreparedTreeMACLAdmissionDoesNotPermitReplacementExtras(t *testing.T) {
	err := verifyObservedXattrs(
		preservedMetadata{xattrs: map[string][]byte{"com.apple.macl": []byte("observed")}},
		preservedMetadata{xattrs: make(map[string][]byte), replacement: true},
		isAllowedReplacementXattr,
	)
	if !isUnsupported(err) {
		t.Fatalf("new MACL replacement extra was admitted: %v", err)
	}
}
