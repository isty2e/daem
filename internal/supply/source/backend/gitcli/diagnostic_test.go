package gitcli

import (
	"strings"
	"testing"
)

func TestSanitizeGitDiagnosticRedactsAndBoundsUntrustedStderr(t *testing.T) {
	t.Parallel()

	secret := "synthetic-secret"
	diagnostic := "fatal:\x1b[31m fetch https://user:" + secret + "@example.com/repo.git\n" +
		"ssh://principal@example.com/repo.git\u200b " +
		"https://example.com/repo.git?access_token=" + secret + " " +
		"Authorization: Bearer " + secret + "\ntoken=" + secret
	got := sanitizeGitDiagnostic(diagnostic)

	if strings.Contains(got, secret) || strings.Contains(got, "user:") || strings.Contains(got, "Bearer "+secret) {
		t.Fatalf("sanitized diagnostic disclosed a credential: %q", got)
	}
	if strings.ContainsAny(got, "\x1b\n\r") || strings.ContainsRune(got, '\u200b') {
		t.Fatalf("sanitized diagnostic retained control or format characters: %q", got)
	}
	if !strings.Contains(got, "https://<redacted>@example.com/repo.git") {
		t.Fatalf("sanitized diagnostic = %q, want redacted URL", got)
	}
	if !strings.Contains(got, "ssh://<redacted>@example.com/repo.git") {
		t.Fatalf("sanitized diagnostic = %q, want conservative SSH userinfo redaction", got)
	}
	if strings.Count(got, "[REDACTED]") < 3 {
		t.Fatalf("sanitized diagnostic = %q, want query, header, and key-value redaction", got)
	}

	oversized := sanitizeGitDiagnostic(strings.Repeat("x", maxGitDiagnosticRunes+100))
	if len([]rune(oversized)) != maxGitDiagnosticRunes+len(" [truncated]") || !strings.HasSuffix(oversized, "[truncated]") {
		t.Fatalf("bounded diagnostic length/suffix = %d/%q", len([]rune(oversized)), oversized[len(oversized)-len("[truncated]"):])
	}

	invalidUTF8 := sanitizeGitDiagnostic(string([]byte{'a', 0xff, 'b'}))
	if invalidUTF8 != "a�b" {
		t.Fatalf("invalid UTF-8 diagnostic = %q, want replacement rune", invalidUTF8)
	}

	hostileTerminal := sanitizeGitDiagnostic("visible\x1b]8;;https://example.test\x07concealed")
	if hostileTerminal != "[REDACTED]" {
		t.Fatalf("hostile terminal diagnostic = %q, want complete field redaction", hostileTerminal)
	}
}

func TestGitDiagnosticBufferBoundsRawCapture(t *testing.T) {
	t.Parallel()

	var buffer gitDiagnosticBuffer
	payload := []byte(strings.Repeat("x", maxGitDiagnosticBytes+128))
	written, err := buffer.Write(payload)
	if err != nil || written != len(payload) {
		t.Fatalf("Write = %d, %v; want %d, nil", written, err, len(payload))
	}
	if len(buffer.data) != maxGitDiagnosticBytes || !buffer.Truncated() {
		t.Fatalf("buffer size/truncated = %d/%t", len(buffer.data), buffer.Truncated())
	}
	if got := sanitizeGitDiagnosticCapture(buffer.String(), buffer.Truncated()); !strings.HasSuffix(got, "[truncated]") {
		t.Fatalf("sanitized bounded capture = %q, want truncation marker", got)
	}
}
