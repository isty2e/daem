package directfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicManifestDocumentsDirectFileLimit(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "docs", "manifest.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read public manifest: %v", err)
	}
	required := fmt.Sprintf(
		"Direct regular-file sources are limited to %d MiB.",
		maximumBytes>>20,
	)
	if !strings.Contains(string(content), required) {
		t.Fatalf("public manifest is missing direct-file limit %q", required)
	}
}
