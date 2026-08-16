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
	// Credential-bearing http(s) userinfo is redacted by the shared
	// credential policy with the canonical marker; the conservative
	// git-diagnostic layer additionally redacts non-credential userinfo such
	// as ssh principals with its own marker.
	if !strings.Contains(got, "https://[REDACTED]@example.com/repo.git") {
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

func TestSanitizeGitDiagnosticWithholdsQuotedAuthorizationContinuations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{
			name:  "quoted value with same-line suffix",
			value: `Authorization: "Bearer first" inherited-secret`,
		},
		{
			name:  "prefixed quoted continuation",
			value: "Authorization: Bearer \"first\r\nactual-secret\"",
		},
		{
			name:  "opaque key prefixed quoted continuation",
			value: "ключ: prefix \"first\r\nactual-secret\"",
		},
		{
			name:  "digest quoted continuation",
			value: "Authorization: Digest realm=\"public\", response=\"first\r\nactual-secret\"",
		},
		{
			name:  "token bearer tail",
			value: "token: Bearer actual-secret",
		},
		{
			name:  "password bearer tail",
			value: "password=Bearer actual-secret",
		},
		{
			name:  "secret bearer quoted continuation",
			value: "secret: Bearer \"first\r\nactual-secret\"",
		},
		{
			name:  "unterminated quoted password",
			value: `password="first actual-secret`,
		},
		{
			name:  "bidi override inside credential key",
			value: "to\u202Eken=actual-secret",
		},
		{
			name:  "format control becomes bearer separator",
			value: "Bearer\u200Bactual-secret",
		},
		{
			name:  "scheme-less password locator",
			value: "fatal: git:user:actual-secret@github.com/acme/tool",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := sanitizeGitDiagnostic(test.value)
			if strings.Contains(got, "inherited-secret") || strings.Contains(got, "actual-secret") {
				t.Fatalf("sanitizeGitDiagnostic(%q) = %q, disclosed continuation", test.value, got)
			}
			if !strings.Contains(got, "[REDACTED]") {
				t.Fatalf("sanitizeGitDiagnostic(%q) = %q, want a redaction marker", test.value, got)
			}
		})
	}
}

func TestSanitizeGitDiagnosticClosesSourceAndTransformCredentialSurfaces(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value string
		want  string
	}{
		{
			value: "fatal: git:user:actual-secret@github.com/acme/tool#https://example.test/ref",
			want:  "fatal: git:[REDACTED]@github.com/acme/tool#https://example.test/ref",
		},
		{
			value: "fatal: git:user:actual-secret@github.com",
			want:  "fatal: git:[REDACTED]@github.com",
		},
		{
			value: "fatal: user:actual-secret@short-host:443/acme/tool",
			want:  "fatal: [REDACTED]@short-host:443/acme/tool",
		},
		{
			value: "fatal: user:actual-secret@[2001:db8::1]:443/acme/tool",
			want:  "fatal: [REDACTED]@[2001:db8::1]:443/acme/tool",
		},
		{
			value: "fatal: https://example.test/#git:user:actual-secret@github.com/acme/tool",
			want:  "fatal: https://example.test/#git:[REDACTED]@github.com/acme/tool",
		},
		{
			value: "fatal: user:actual-secret@short-host",
			want:  "fatal: [REDACTED]@short-host",
		},
		{
			value: "fatal: https://example.test/#user:actual-secret@short-host",
			want:  "fatal: https://example.test/#[REDACTED]@short-host",
		},
		{
			value: "fatal: [git:user:actual-secret@short-host]",
			want:  "fatal: [git:[REDACTED]@short-host]",
		},
		{
			value: "fatal: git:user:actual-secret@short-host:repo/path",
			want:  "fatal: git:[REDACTED]@short-host:repo/path",
		},
		{
			value: "fatal: user:actual-secret@short+host",
			want:  "fatal: [REDACTED]@short+host",
		},
		{
			value: "fatal: [git:user:actual-secret@short-host]: denied",
			want:  "fatal: [git:[REDACTED]@short-host]: denied",
		},
		{
			value: "fatal: git:user:actual-secret@[2001:db8::1",
			want:  "fatal: git:[REDACTED]@[2001:db8::1",
		},
	} {
		if got := sanitizeGitDiagnostic(test.value); got != test.want {
			t.Errorf("scheme-less diagnostic = %q, want %q", got, test.want)
		}
	}

	for _, separator := range []string{"\u200B", "\u2060"} {
		got := sanitizeGitDiagnostic("Bearer" + separator + "actual-secret")
		if got != "Bearer [REDACTED]" {
			t.Fatalf("format-normalized diagnostic = %q, want bearer value redacted", got)
		}
	}
	for _, separator := range []string{
		"%E2%80%8B",
		"%E2%81%A0",
		"%25E2%2580%258B",
	} {
		got := sanitizeGitDiagnostic("é Bearer" + separator + "actual-secret")
		want := "é Bearer" + separator + "[REDACTED]"
		if got != want {
			t.Fatalf("encoded format-normalized diagnostic = %q, want %q", got, want)
		}
	}

	got := sanitizeGitDiagnostic("Bearer\nactual-secret")
	if got != "Bearer [REDACTED]" {
		t.Fatalf("line-normalized diagnostic = %q, want bearer value redacted", got)
	}
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "Bearer actual\u200bsecret", want: "Bearer [REDACTED]"},
		{value: "Bearer actual%E2%80%8Bsecret", want: "Bearer [REDACTED]"},
		{value: "Bearer actual%25E2%2580%258Bsecret", want: "Bearer [REDACTED]"},
		{value: "Bea\u200brer actual-secret", want: "Bea rer [REDACTED]"},
		{value: "Bea%E2%80%8Brer actual-secret", want: "Bea%E2%80%8Brer [REDACTED]"},
		{value: "Bea%25E2%2580%258Brer actual-secret", want: "Bea%25E2%2580%258Brer [REDACTED]"},
		{value: "Bearer%00actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea%09rer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea%0Arer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea%0Drer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea\trer actual-secret leftover", want: "[REDACTED]"},
		{value: "Bea\nrer actual-secret leftover", want: "[REDACTED]"},
		{value: "--to%00ken=actual-secret leftover", want: "--to%00ken=[REDACTED]"},
	} {
		if got := sanitizeGitDiagnostic(test.value); got != test.want {
			t.Errorf("format-control bearer diagnostic = %q, want %q", got, test.want)
		}
	}
}

func TestSanitizeGitDiagnosticFailsClosedOnInvalidUTF8BearerForms(t *testing.T) {
	for _, value := range []string{
		"Bearer actual" + string([]byte{0xff}) + "secret",
		"Bea" + string([]byte{0xff}) + "rer actual-secret",
		"Bearer actual%FFsecret",
		"Bea%FFrer actual-secret",
	} {
		got := sanitizeGitDiagnostic(value)
		if strings.Contains(got, "actual") || strings.Contains(got, "secret") {
			t.Errorf("sanitizeGitDiagnostic(%q) = %q, disclosed invalid UTF-8 Bearer field", value, got)
		}
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

func TestSanitizeGitDiagnosticBoundsReplacementExpansion(t *testing.T) {
	t.Parallel()

	repeated := strings.Repeat("Bearer a ", 500)
	runes := []rune(repeated)
	if len(runes) > 4095 {
		repeated = string(runes[:4095])
	}
	got := sanitizeGitDiagnostic(repeated)
	if strings.Contains(got, "Bearer a") {
		t.Fatalf("sanitizeGitDiagnostic disclosed bearer tokens: %q", got[:min(120, len(got))])
	}
	limit := maxGitDiagnosticRunes + len(" [truncated]")
	if n := len([]rune(got)); n > limit {
		t.Fatalf("repeated bearer diagnostic length = %d, want <= %d", n, limit)
	}

	nearLimit := strings.Repeat("x", maxGitDiagnosticRunes-8) + " ssh://ab@example.com"
	got = sanitizeGitDiagnostic(nearLimit)
	if strings.Contains(got, "ssh://ab@") {
		t.Fatalf("sanitizeGitDiagnostic disclosed ssh userinfo: %q", got[len(got)-80:])
	}
	if n := len([]rune(got)); n > limit {
		t.Fatalf("ssh userinfo expansion length = %d, want <= %d", n, limit)
	}
}
