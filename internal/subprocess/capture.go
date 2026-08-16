package subprocess

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/credentialtext"
)

// BoundedBuffer accumulates at most limit raw bytes while reporting whether
// any input was omitted. Sanitization remains a separate CapturePolicy operation.
type BoundedBuffer struct {
	limit     int
	data      []byte
	truncated bool
}

// NewBoundedBuffer constructs an empty bounded raw-output accumulator.
func NewBoundedBuffer(limit int) *BoundedBuffer {
	if limit < 0 {
		limit = 0
	}
	return &BoundedBuffer{limit: limit}
}

// Write implements io.Writer and always consumes the caller's full input.
func (buffer *BoundedBuffer) Write(payload []byte) (int, error) {
	written := len(payload)
	remaining := buffer.limit - len(buffer.data)
	if remaining <= 0 {
		if len(payload) != 0 {
			buffer.truncated = true
		}
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

// WriteString implements io.StringWriter without first copying the complete
// input into a byte slice.
func (buffer *BoundedBuffer) WriteString(payload string) (int, error) {
	written := len(payload)
	remaining := buffer.limit - len(buffer.data)
	if remaining <= 0 {
		if len(payload) != 0 {
			buffer.truncated = true
		}
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

// String returns the captured raw bytes as text for subsequent sanitation.
func (buffer *BoundedBuffer) String() string {
	return string(buffer.data)
}

// Truncated reports whether any non-empty input exceeded the bound.
func (buffer *BoundedBuffer) Truncated() bool {
	return buffer.truncated
}

const (
	redactionMarker  = "[REDACTED]"
	truncationMarker = "\n[truncated]"
)

// CapturePolicy is one immutable bounded-redaction policy. A non-positive limit keeps
// no payload text; callers own any executor-specific default.
type CapturePolicy struct {
	secrets []string
	limit   int
}

// CaptureResult is sanitized bounded text plus the facts needed by executor-local
// result models.
type CaptureResult struct {
	text      string
	truncated bool
	redacted  bool
}

// NewCapturePolicy canonicalizes explicit secret values once for repeated fields.
func NewCapturePolicy(secrets []string, limit int) CapturePolicy {
	if limit < 0 {
		limit = 0
	}
	return CapturePolicy{
		secrets: canonicalSecrets(secrets),
		limit:   limit,
	}
}

// Sanitize removes safe styling, rejects display-rewriting terminal controls,
// redacts secret-shaped fragments and complete secrets, conservatively redacts
// a secret prefix at an upstream truncation boundary, normalizes invalid UTF-8,
// and then applies the policy's rune limit.
func (policy CapturePolicy) Sanitize(value string, upstreamTruncated bool) CaptureResult {
	return policy.SanitizeUsing(value, upstreamTruncated, nil, nil)
}

// SanitizeUsing is Sanitize with optional extra redaction spans and a
// presentation transform. Extra spans are collected from the
// control-sanitized text, the same observation surface as shared credential
// detection. The transformed surface is sanitized and inspected again before
// the rune bound: a display rewrite may expose credential grammar that was not
// present in the raw spelling, but it cannot make that grammar public.
func (policy CapturePolicy) SanitizeUsing(
	value string,
	upstreamTruncated bool,
	spans func(string) []credentialtext.Span,
	beforeBound func(string) string,
) CaptureResult {
	text := value
	boundaryRedacted := false
	if upstreamTruncated {
		text, boundaryRedacted = redactTruncatedSecretSuffix(text, policy.secrets)
	}
	text, controlRedacted := sanitizeTerminalControls(strings.ToValidUTF8(text, "\uFFFD"))
	var extra []credentialtext.Span
	if spans != nil {
		extra = spans(text)
	}
	// Structural, userinfo, explicit-secret, and caller spans are redacted in
	// one pass over the raw and decoded forms, so a later rewrite cannot
	// destroy the grammar a detector still needs.
	text, fragmentRedacted := redactSecretFragments(text, policy.secrets, extra)
	if upstreamTruncated {
		var normalizedBoundaryRedacted bool
		text, normalizedBoundaryRedacted = redactTruncatedSecretSuffix(text, policy.secrets)
		boundaryRedacted = boundaryRedacted || normalizedBoundaryRedacted
	}
	if beforeBound != nil {
		text = beforeBound(text)
		var transformedControlRedacted bool
		text, transformedControlRedacted = sanitizeTerminalControls(
			strings.ToValidUTF8(text, "\uFFFD"),
		)
		controlRedacted = controlRedacted || transformedControlRedacted
		var transformedExtra []credentialtext.Span
		if spans != nil {
			transformedExtra = spans(text)
		}
		var transformedFragmentRedacted bool
		text, transformedFragmentRedacted = redactSecretFragments(
			text,
			policy.secrets,
			transformedExtra,
		)
		fragmentRedacted = fragmentRedacted || transformedFragmentRedacted
		if upstreamTruncated {
			var transformedBoundaryRedacted bool
			text, transformedBoundaryRedacted = redactTruncatedSecretSuffix(
				text,
				policy.secrets,
			)
			boundaryRedacted = boundaryRedacted || transformedBoundaryRedacted
		}
	}
	text, bounded := boundRunes(text, policy.limit)
	return CaptureResult{
		text:      text,
		truncated: upstreamTruncated || bounded,
		redacted:  controlRedacted || fragmentRedacted || boundaryRedacted,
	}
}

// Text returns sanitized bounded text.
func (result CaptureResult) Text() string {
	return result.text
}

// Truncated reports upstream or policy-local truncation.
func (result CaptureResult) Truncated() bool {
	return result.truncated
}

// Redacted reports whether unsafe display controls, explicit secrets, or
// secret-shaped content were removed. Removing SGR styling alone is not a
// redaction.
func (result CaptureResult) Redacted() bool {
	return result.redacted
}

func canonicalSecrets(secrets []string) []string {
	seen := make(map[string]struct{}, len(secrets)*2)
	result := make([]string, 0, len(secrets)*2)
	appendSecret := func(secret string) {
		if secret == "" {
			return
		}
		if _, exists := seen[secret]; exists {
			return
		}
		seen[secret] = struct{}{}
		result = append(result, secret)
	}
	for _, secret := range secrets {
		appendSecret(secret)
		normalized, _ := sanitizeTerminalControls(strings.ToValidUTF8(secret, "\uFFFD"))
		if normalized != redactionMarker && normalized != secret {
			appendSecret(normalized)
		}
	}
	sort.SliceStable(result, func(left int, right int) bool {
		return len(result[left]) > len(result[right])
	})
	return result
}

// sanitizeTerminalControls strips only SGR styling, which does not rearrange
// visible text. Other terminal controls can rewrite or conceal output, so the
// field is discarded instead of attempting an incomplete terminal emulator.
func sanitizeTerminalControls(value string) (string, bool) {
	result := make([]byte, 0, len(value))
	for index := 0; index < len(value); {
		current := value[index]
		switch {
		case current == '\x1b':
			next, ok := consumeSGR(value, index)
			if !ok {
				return redactionMarker, true
			}
			index = next
		case current == '\r':
			if index+1 >= len(value) || value[index+1] != '\n' {
				return redactionMarker, true
			}
			result = append(result, current)
			index++
		case current == '\n' || current == '\t':
			result = append(result, current)
			index++
		case current < 0x20 || current == 0x7f:
			return redactionMarker, true
		case current < utf8.RuneSelf:
			result = append(result, current)
			index++
		default:
			decoded, size := utf8.DecodeRuneInString(value[index:])
			if decoded >= '\u0080' && decoded <= '\u009f' ||
				unicode.Is(unicode.Bidi_Control, decoded) {
				return redactionMarker, true
			}
			result = append(result, value[index:index+size]...)
			index += size
		}
	}
	return string(result), false
}

func consumeSGR(value string, escapeIndex int) (int, bool) {
	if escapeIndex+2 > len(value) || value[escapeIndex+1] != '[' {
		return 0, false
	}
	index := escapeIndex + 2
	for index < len(value) && value[index] >= 0x30 && value[index] <= 0x3f {
		index++
	}
	for index < len(value) && value[index] >= 0x20 && value[index] <= 0x2f {
		index++
	}
	if index >= len(value) || value[index] != 'm' {
		return 0, false
	}
	return index + 1, true
}

func redactTruncatedSecretSuffix(value string, secrets []string) (string, bool) {
	for _, secret := range secrets {
		maximum := min(len(value), len(secret)-1)
		for size := maximum; size > 0; size-- {
			prefix := secret[:size]
			if before, ok := strings.CutSuffix(value, prefix); ok {
				return before + redactionMarker, true
			}
		}
	}
	// An upstream truncation can cut a percent-encoded secret mid-escape, so
	// the decoded inspection form is checked for a secret prefix too and the
	// match is mapped back to its raw span. A form that does not stabilize is
	// uninspectable and fails closed, matching the structural redaction
	// contract.
	decoded, offsets, stable := credentialtext.CanonicalDecode(value)
	if !stable {
		return redactionMarker, true
	}
	if offsets != nil && decoded != value {
		for _, secret := range secrets {
			maximum := min(len(decoded), len(secret)-1)
			for size := maximum; size > 0; size-- {
				prefix := secret[:size]
				if before, ok := strings.CutSuffix(decoded, prefix); ok {
					return value[:int(offsets[len(before)])] + redactionMarker, true
				}
			}
		}
	}
	return value, false
}

func redactSecretFragments(value string, secrets []string, extra []credentialtext.Span) (string, bool) {
	if len(extra) == 0 {
		return credentialtext.Redact(value, redactionMarker, secrets)
	}
	return credentialtext.RedactWithSpans(value, redactionMarker, secrets, extra)
}

func boundRunes(value string, limit int) (string, bool) {
	runes := []rune(value)
	if len(runes) <= limit {
		return value, false
	}
	return string(runes[:limit]) + truncationMarker, true
}

type sanitizedCapture struct {
	stdout          string
	stderr          string
	stdoutTruncated bool
	stderrTruncated bool
	redacted        bool
	errorDetail     string
}

func sanitizeCapture(result CommandResult, secrets []string, limit int) sanitizedCapture {
	if limit <= 0 {
		limit = DefaultCommandOutputLimit
	}
	policy := NewCapturePolicy(secrets, limit)
	stdout := policy.Sanitize(result.Stdout, result.StdoutTruncated)
	stderr := policy.Sanitize(result.Stderr, result.StderrTruncated)

	var errorDetail CaptureResult
	if result.Err != nil {
		errorDetail = policy.Sanitize(result.Err.Error(), false)
	}

	return sanitizedCapture{
		stdout:          stdout.Text(),
		stderr:          stderr.Text(),
		stdoutTruncated: stdout.Truncated(),
		stderrTruncated: stderr.Truncated(),
		redacted:        stdout.Redacted() || stderr.Redacted() || errorDetail.Redacted(),
		errorDetail:     errorDetail.Text(),
	}
}
