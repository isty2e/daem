package gitcli

import (
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/credentialtext"
	"github.com/isty2e/daem/internal/subprocess"
)

const (
	maxGitDiagnosticRunes = 4096
	maxGitDiagnosticBytes = maxGitDiagnosticRunes * 4
)

var (
	gitURLUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/@\s]+@`)
	gitQuerySecretPattern = regexp.MustCompile(
		`(?i)([?&](?:access[_-]?token|api[_-]?key|auth(?:orization)?|password|passwd|secret|token)=)[^&#\s]+`,
	)
	gitAuthorizationPattern = regexp.MustCompile(
		`(?i)(\b(?:authorization|proxy-authorization)\s*[:=]\s*)(?:(?:basic|bearer)\s+)?[^\s,;]+`,
	)
)

func gitCommandErrorWithCapture(
	policy subprocess.CapturePolicy,
	err error,
	stderr string,
	truncated bool,
) error {
	if stderrText := sanitizeGitDiagnosticCaptureWithPolicy(policy, stderr, truncated); stderrText != "" {
		return &capturedGitCommandError{diagnostic: stderrText, cause: err}
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if stderrText := sanitizeGitDiagnosticCaptureWithPolicy(policy, string(exitError.Stderr), false); stderrText != "" {
			return &capturedGitCommandError{diagnostic: stderrText, cause: err}
		}
	}

	return err
}

type capturedGitCommandError struct {
	diagnostic string
	cause      error
}

func (err *capturedGitCommandError) Error() string {
	if err == nil {
		return ""
	}
	return err.diagnostic
}

func (err *capturedGitCommandError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func sanitizeGitDiagnostic(value string) string {
	return sanitizeGitDiagnosticCapture(value, false)
}

func sanitizeGitDiagnosticCapture(value string, truncated bool) string {
	policy := subprocess.NewCapturePolicy(nil, maxGitDiagnosticRunes)
	return sanitizeGitDiagnosticCaptureWithPolicy(policy, value, truncated)
}

func sanitizeGitDiagnosticCaptureWithPolicy(
	policy subprocess.CapturePolicy,
	value string,
	truncated bool,
) string {
	// Observation must keep the original key grammar. Format-control
	// rewriting before redaction can split credential keys and also erase
	// bidi/control signals the shared sanitizer fail-closes on. Display
	// normalization of remaining Cf runs after span collection.
	result := policy.SanitizeUsing(
		value,
		truncated,
		gitDiagnosticSpans,
		func(text string) string {
			text = normalizeGitCredentialSeparators(text)
			return strings.Join(strings.Fields(text), " ")
		},
	)
	text := result.Text()
	if truncated && !strings.HasSuffix(text, "[truncated]") {
		text += " [truncated]"
	}
	return text
}

func gitDiagnosticSpans(value string) []credentialtext.Span {
	forms := []gitDiagnosticObservation{{text: value}}
	decoded, offsets, stable := credentialtext.CanonicalDecode(value)
	if stable && offsets != nil && decoded != value {
		forms = append(forms, gitDiagnosticObservation{text: decoded, rawOffsets: offsets})
	}
	var spans []credentialtext.Span
	for _, form := range forms {
		for _, span := range gitDiagnosticSpansOnce(form.text) {
			if span.Start < 0 || span.End > len(form.text) || span.Start >= span.End {
				continue
			}
			if form.rawOffsets != nil {
				span.Start = int(form.rawOffsets[span.Start])
				span.End = int(form.rawOffsets[span.End])
			}
			spans = append(spans, span)
		}
	}
	return spans
}

// gitDiagnosticObservation is one bounded inspection form whose byte
// boundaries map directly to the original diagnostic. Public rendering always
// applies the resulting spans to the original text.
type gitDiagnosticObservation struct {
	text       string
	rawOffsets []int32
}

// normalizeGitCredentialSeparators makes remaining format controls inert for
// display. Credential spans are collected first from the logical field, so a
// rewrite can neither split a secret nor create a newly visible suffix.
func normalizeGitCredentialSeparators(value string) string {
	firstFormatControl := -1
	for index := 0; index < len(value); {
		character, width := utf8.DecodeRuneInString(value[index:])
		if unicode.In(character, unicode.Cf) {
			firstFormatControl = index
			break
		}
		index += width
	}
	if firstFormatControl < 0 {
		return value
	}

	var normalized strings.Builder
	normalized.Grow(len(value))
	normalized.WriteString(value[:firstFormatControl])
	for index := firstFormatControl; index < len(value); {
		character, width := utf8.DecodeRuneInString(value[index:])
		if unicode.In(character, unicode.Cf) {
			normalized.WriteByte(' ')
			index += width
			continue
		}
		normalized.WriteString(value[index : index+width])
		index += width
	}
	return normalized.String()
}

func gitDiagnosticSpansOnce(value string) []credentialtext.Span {
	var spans []credentialtext.Span
	spans = append(spans, gitRegexSecretSpans(gitQuerySecretPattern, value)...)
	spans = append(spans, gitRegexSecretSpans(gitAuthorizationPattern, value)...)
	spans = append(spans, gitTransportUserSpans(value)...)
	return spans
}

func gitRegexSecretSpans(pattern *regexp.Regexp, value string) []credentialtext.Span {
	matches := pattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return nil
	}
	spans := make([]credentialtext.Span, 0, len(matches))
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		spans = append(spans, credentialtext.Span{Start: match[3], End: match[1]})
	}
	return spans
}

func gitTransportUserSpans(value string) []credentialtext.Span {
	matches := gitURLUserinfoPattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return nil
	}
	var spans []credentialtext.Span
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		scheme := strings.ToLower(strings.TrimSuffix(value[match[2]:match[3]], "://"))
		userinfoStart, userinfoEnd := match[3], match[1]-1
		if userinfoStart >= userinfoEnd {
			continue
		}
		userinfo := value[userinfoStart:userinfoEnd]
		if userinfo == "[REDACTED]" || strings.Contains(userinfo, ":") {
			continue
		}
		if scheme == "http" || scheme == "https" ||
			strings.HasSuffix(scheme, "+http") || strings.HasSuffix(scheme, "+https") {
			continue
		}
		spans = append(spans, credentialtext.Span{
			Start:  userinfoStart,
			End:    userinfoEnd,
			Marker: "<redacted>",
		})
	}
	return spans
}

type gitDiagnosticBuffer struct {
	data      []byte
	truncated bool
}

func (buffer *gitDiagnosticBuffer) Write(payload []byte) (int, error) {
	written := len(payload)
	remaining := maxGitDiagnosticBytes - len(buffer.data)
	if remaining <= 0 {
		buffer.truncated = buffer.truncated || len(payload) > 0
		return written, nil
	}
	if len(payload) > remaining {
		buffer.data = append(buffer.data, payload[:remaining]...)
		buffer.truncated = true
		return written, nil
	}
	buffer.data = append(buffer.data, payload...)
	return written, nil
}

func (buffer *gitDiagnosticBuffer) String() string {
	return string(buffer.data)
}

func (buffer *gitDiagnosticBuffer) Truncated() bool {
	return buffer.truncated
}
