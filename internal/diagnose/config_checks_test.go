package diagnose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/encoding/tomlstrict"
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

func TestConfigFileCheckRejectsDeepTOMLBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(nestedDoctorInlineTables(tomlstrict.MaximumDepth)), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityWarn,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "depth") {
		t.Fatalf("check = %#v, want structure-budget error not syntax warn", check)
	}
}

func TestConfigFileCheckAcceptsExactTOMLDepth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(nestedDoctorInlineTables(tomlstrict.MaximumDepth-1)), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckOK {
		t.Fatalf("check = %#v, want parseable exact-depth toml", check)
	}
}

func TestConfigFileCheckRejectsDeepJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, nestedDoctorJSONObjects(tomlstrict.MaximumDepth+1), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=claude-code config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatJSON,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "depth") {
		t.Fatalf("check = %#v, want JSON depth error", check)
	}
}

func TestConfigFileCheckRejectsDeepJSONEvenWhenSyntaxIsWarning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, nestedDoctorJSONObjects(tomlstrict.MaximumDepth+1), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=opencode config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatJSON,
		SyntaxErrorSeverity: findings.SeverityWarn,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "depth") {
		t.Fatalf("check = %#v, want JSON depth error not syntax warn", check)
	}
}

func TestConfigFileCheckWarnsDuplicateJSONKeysForWarningTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(`{"model":"a","model":"b"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=opencode config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatJSON,
		SyntaxErrorSeverity: findings.SeverityWarn,
	})
	if check.Status != findings.CheckWarn ||
		!strings.Contains(check.Detail, "strict parse") ||
		!strings.Contains(check.Detail, "duplicate") {
		t.Fatalf("check = %#v, want duplicate-key syntax warning", check)
	}
}

func TestConfigFileCheckErrorsDuplicateJSONKeysForErrorTargets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"model":"a","model":"b"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=claude-code config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatJSON,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "duplicate") {
		t.Fatalf("check = %#v, want duplicate-key error", check)
	}
}

func TestConfigFileCheckAcceptsSpaceSeparatedTOMLDatetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("checked_at = 1987-07-05 17:45:00Z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityError,
	})
	if check.Status != findings.CheckOK || !strings.Contains(check.Detail, "is parseable") {
		t.Fatalf("check = %#v, want parseable space-separated datetime", check)
	}
}

func TestConfigFileCheckRejectsDottedInlinePathBeforeDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	content := "k = { " + strings.Repeat("a.", tomlstrict.MaximumDepth-1) + "a = 1 }\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	check := configFileCheck(context.Background(), "target=codex config_file", doctorConfigFile{
		Path:                path,
		Format:              ConfigFormatTOML,
		SyntaxErrorSeverity: findings.SeverityWarn,
	})
	if check.Status != findings.CheckError || !strings.Contains(check.Detail, "depth") {
		t.Fatalf("check = %#v, want semantic-path error not syntax warn", check)
	}
}

func nestedDoctorInlineTables(depth int) string {
	var builder strings.Builder
	builder.WriteString("k = ")
	for range depth {
		builder.WriteString("{k = ")
	}
	builder.WriteByte('1')
	for range depth {
		builder.WriteByte('}')
	}
	builder.WriteByte('\n')
	return builder.String()
}

func nestedDoctorJSONObjects(depth int) []byte {
	var builder strings.Builder
	for range depth {
		builder.WriteString(`{"a":`)
	}
	builder.WriteByte('1')
	for range depth {
		builder.WriteByte('}')
	}
	return []byte(builder.String())
}
