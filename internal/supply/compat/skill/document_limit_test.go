package skillcompat

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func TestSkillDocumentLimitAcceptsExactRawBoundaryWithBOMAndCRLF(t *testing.T) {
	prefix := []byte("\xef\xbb\xbf---\r\nname: oracle\r\ndescription: Review.\r\n---\r\n")
	content := append(prefix, bytes.Repeat([]byte("x"), int(MaximumSkillDocumentBytes)-len(prefix))...)
	root := writeSkillFrontmatter(t, "oracle", string(content))

	frontmatter, err := loadSkillFrontmatterPath(t, root)
	if err != nil {
		t.Fatalf("LoadSkillFrontmatter exact boundary: %v", err)
	}
	if frontmatter.Name != "oracle" || frontmatter.Description != "Review." {
		t.Fatalf("frontmatter = %#v, want BOM/CRLF values", frontmatter)
	}
}

func TestSkillDocumentLimitRejectsSparseFileOneByteOverBeforeParsing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "oracle")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "SKILL.md")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaximumSkillDocumentBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = loadSkillFrontmatterPath(t, root)
	requireSkillDocumentLimit(t, err, MaximumSkillDocumentBytes+1)
}

func TestParseSkillFrontmatterRejectsOversizedScalarBeforeYAMLDecode(t *testing.T) {
	content := append(
		[]byte("---\ndescription: |-\n  "),
		bytes.Repeat([]byte("x"), int(MaximumSkillDocumentBytes))...,
	)
	_, err := ParseSkillFrontmatter(content)
	requireSkillDocumentLimit(t, err, int64(len(content)))
}

func TestDiagnosticsClassifiesOversizedSkillDocument(t *testing.T) {
	root := filepath.Join(t.TempDir(), "oracle")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "SKILL.md")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(MaximumSkillDocumentBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	view := openSkillView(t, root)

	diagnostics := Diagnostics(
		context.Background(),
		view,
		artifact.SourceID("local:skills/oracle?mode=vendor"),
		"oracle",
		target.TargetCodex,
	)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v, want one limit diagnostic", diagnostics)
	}
	diagnostic := diagnostics[0]
	if diagnostic.Severity != SeverityError ||
		diagnostic.Axis != AxisArtifact ||
		diagnostic.Code != "skill-document-too-large" {
		t.Fatalf("diagnostic = %#v, want stable oversized artifact classification", diagnostic)
	}
	if !bytes.Contains([]byte(diagnostic.Message), []byte("is at least 1048577 bytes")) {
		t.Fatalf("diagnostic message = %q, want lower-bound byte wording", diagnostic.Message)
	}
}

func TestReadSkillDocumentRejectsUnrelatedArtifactPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "oracle")
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "model.bin"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	view := openSkillView(t, root)

	if _, err := ReadSkillDocument(context.Background(), view, "assets/model.bin"); err == nil {
		t.Fatal("ReadSkillDocument accepted an unrelated artifact path")
	}
}

func requireSkillDocumentLimit(t *testing.T, err error, observed int64) {
	t.Helper()
	var limitErr *SkillDocumentLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error = %v, want SkillDocumentLimitError", err)
	}
	if !errors.Is(err, ErrSkillDocumentTooLarge) {
		t.Fatalf("error = %v, want ErrSkillDocumentTooLarge", err)
	}
	if limitErr.Limit() != MaximumSkillDocumentBytes || limitErr.Observed() != observed {
		t.Fatalf(
			"limit error = limit %d observed %d, want limit %d observed %d",
			limitErr.Limit(),
			limitErr.Observed(),
			MaximumSkillDocumentBytes,
			observed,
		)
	}
}
