package diagnose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/findings"
)

func TestConfigFileCheckReportsMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	check := configFileCheck(context.Background(), "target=claude-code config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatJSON,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckWarn || !strings.Contains(check.Detail, "is missing") {
		t.Fatalf("check = %#v, want missing warning", check)
	}
}

func TestConfigFileCheckRejectsOversizedFileWithoutDecoding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	payload := make([]byte, doctorHostConfigMaximumBytes+1)
	for index := range payload {
		payload[index] = 'a'
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "exceeds") {
		t.Fatalf("check = %#v, want byte-ceiling error", check)
	}
}

func TestConfigFileCheckHonorsCallerCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("value = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	check := configFileCheck(ctx, "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "context canceled") {
		t.Fatalf("check = %#v, want canceled read", check)
	}
}

func TestConfigFileCheckParsesBoundedTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("model = \"gpt\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckOK || !strings.Contains(check.Detail, "is parseable") {
		t.Fatalf("check = %#v, want parseable toml", check)
	}
}
