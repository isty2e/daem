package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicManifestDocumentsArchiveBudgets(t *testing.T) {
	budget := defaultBudget()
	manifest := readArchivePublicManifest(t)
	required := []string{
		fmt.Sprintf("| Raw tar or compressed gzip input | %d MiB |", budget.inputBytes>>20),
		fmt.Sprintf("| Decompressed tar stream | %d MiB |", budget.tarStreamBytes>>20),
		fmt.Sprintf("| Total extracted regular-file bytes | %d MiB |", budget.expandedBytes>>20),
		fmt.Sprintf("| One regular file | %d MiB |", budget.entryBytes>>20),
		fmt.Sprintf("| Logical entries | %s |", formatArchiveCount(budget.entryCount)),
		fmt.Sprintf(
			"| Canonical path | %s bytes and %s components |",
			formatArchiveCount(budget.pathBytes),
			formatArchiveCount(budget.pathDepth),
		),
	}
	for _, row := range required {
		if !strings.Contains(manifest, row) {
			t.Errorf("public manifest is missing archive budget row %q", row)
		}
	}
}

func readArchivePublicManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "..", "docs", "manifest.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read public manifest: %v", err)
	}
	return string(content)
}

func formatArchiveCount(value int64) string {
	digits := fmt.Sprintf("%d", value)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}
