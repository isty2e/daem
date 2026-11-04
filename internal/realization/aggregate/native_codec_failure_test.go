package aggregate

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/output"
)

func TestCodecFailureRetainsNativeLocationAndQuotesControlData(t *testing.T) {
	locations := [][]string{nil, {""}, {"/"}, {"~1"}, {"a/b"}, {"a", "b"}, {"."}, {".."}, {"한글", "0"}, {"line\n\r\t\x00\x1b\u202ekey"}}
	seen := make(map[output.KeyPath]bool)
	for _, segments := range locations {
		location, err := output.NewKeyPath(segments...)
		if err != nil {
			t.Fatal(err)
		}
		failure, err := NewCodecFailure(CodecFailureDuplicateKey, location)
		if err != nil || failure.Validate() != nil || failure.Location() != location || failure.Reason() != CodecFailureDuplicateKey {
			t.Fatalf("diagnostic changed native location or reason: %#v, %v", segments, err)
		}
		if seen[failure.Location()] {
			t.Fatal("distinct diagnostic locations collapsed")
		}
		seen[failure.Location()] = true
		if strings.ContainsAny(failure.Error(), "\n\r\t\x00\x1b\u202e") {
			t.Fatalf("native control key became terminal control data: %q", failure.Error())
		}
	}
	if _, err := NewCodecFailure("unknown", output.KeyPath{}); err == nil {
		t.Fatal("diagnostic admitted an unknown failure class")
	}
	if (CodecFailure{}).Validate() == nil {
		t.Fatal("zero diagnostic was accepted")
	}
}

func TestLegacyCodecFailureAdmissionDoesNotBecomeNativePointerParsing(t *testing.T) {
	for _, test := range []struct {
		path ContentPath
		keys []string
	}{
		{path: ""},
		{path: "/hooks", keys: []string{"hooks"}},
		{path: "/mcp_servers/server-1", keys: []string{"mcp_servers", "server-1"}},
	} {
		failure, err := NewCodecFailureFromLegacy(CodecFailureCanonicalInvalid, test.path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := output.NewKeyPath(test.keys...)
		if err != nil || failure.Location() != want {
			t.Fatalf("legacy diagnostic changed location: %q, %v", test.path, err)
		}
	}
	for _, path := range []ContentPath{"hooks", "/", "/a~1b", "/a~0b", "/a%2Fb", "/a/../b", "/line\nkey"} {
		if _, err := NewCodecFailureFromLegacy(CodecFailureDuplicateKey, path); err == nil {
			t.Fatalf("legacy diagnostic grammar was widened for %q", path)
		}
	}
}
