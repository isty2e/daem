package opencodeorder_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe/relation/opencodeorder"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
)

func TestNewDocumentRejectsInconsistentImages(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "opencode.json")
	for _, tc := range []struct {
		name    string
		kind    opencodeconfig.ConfigKind
		path    string
		content []byte
		exists  bool
	}{
		{"absent with content", opencodeconfig.ConfigServer, path, []byte(`{}`), false},
		{"relative path", opencodeconfig.ConfigServer, "opencode.json", nil, false},
		{"unclean path", opencodeconfig.ConfigServer, directory + "/./opencode.json", nil, false},
		{"wrong family", opencodeconfig.ConfigTUI, path, nil, false},
		{"unknown family", "other", path, nil, false},
		{"noncandidate", opencodeconfig.ConfigServer, filepath.Join(directory, "other.jsonc"), []byte(`{}`), true},
		{"malformed content", opencodeconfig.ConfigServer, path, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := opencodeorder.NewDocument(tc.kind, tc.path, tc.content, tc.exists); err == nil {
				t.Fatal("inconsistent supplied document was accepted")
			}
		})
	}
}

func TestNewDocumentChecksByteLimitBeforeCopying(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	content := make([]byte, opencodeorder.MaximumConfigBytes+1)
	if _, err := opencodeorder.NewDocument(opencodeconfig.ConfigServer, path, content, true); err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("expected size refusal before parsing: %v", err)
	}
	result := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if _, err := opencodeorder.NewDocument(opencodeconfig.ConfigServer, path, content, true); err == nil {
				b.Fatal("oversized supplied document was accepted")
			}
		}
	})
	if allocated := result.AllocedBytesPerOp(); allocated >= int64(len(content)) {
		t.Fatalf("oversized refusal allocated %d bytes for %d supplied bytes", allocated, len(content))
	}
}
