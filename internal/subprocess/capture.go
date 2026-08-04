package subprocess

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
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

var assignmentKeyPattern = regexp.MustCompile(
	`(?i)\b([a-z][a-z0-9_-]{0,127})(["']?)(\s*[:=]\s*)`,
)

type credentialValueSpan uint8

const (
	credentialValueNone credentialValueSpan = iota
	credentialValueToken
	credentialValueLine
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
	text := value
	boundaryRedacted := false
	if upstreamTruncated {
		text, boundaryRedacted = redactTruncatedSecretSuffix(text, policy.secrets)
	}
	text, controlRedacted := sanitizeTerminalControls(strings.ToValidUTF8(text, "\uFFFD"))
	// Structural redaction comes first so an explicit value equal to a key such
	// as "token" cannot erase the key before its associated value is removed.
	text, fragmentRedacted := redactSecretFragments(text)
	text, explicitRedacted := redactExplicitSecrets(text, policy.secrets)
	if upstreamTruncated {
		var normalizedBoundaryRedacted bool
		text, normalizedBoundaryRedacted = redactTruncatedSecretSuffix(text, policy.secrets)
		boundaryRedacted = boundaryRedacted || normalizedBoundaryRedacted
	}
	text, bounded := boundRunes(text, policy.limit)
	return CaptureResult{
		text:      text,
		truncated: upstreamTruncated || bounded,
		redacted:  controlRedacted || fragmentRedacted || explicitRedacted || boundaryRedacted,
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

func redactExplicitSecrets(value string, secrets []string) (string, bool) {
	result := value
	redacted := false
	for _, secret := range secrets {
		if !strings.Contains(result, secret) {
			continue
		}
		result = strings.ReplaceAll(result, secret, redactionMarker)
		redacted = true
	}
	return result, redacted
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
	return value, false
}

func redactSecretFragments(value string) (string, bool) {
	var result strings.Builder
	result.Grow(len(value))
	cursor := 0
	searchStart := 0
	redacted := false
	for searchStart < len(value) {
		match := assignmentKeyPattern.FindStringSubmatchIndex(value[searchStart:])
		if len(match) != 8 {
			break
		}
		for index := range match {
			match[index] += searchStart
		}
		searchStart = match[1]
		span := classifyCredentialKey(value[match[2]:match[3]])
		if span == credentialValueNone {
			continue
		}
		valueStart := match[1]
		if valueStart >= len(value) {
			continue
		}
		result.WriteString(value[cursor:valueStart])
		result.WriteString(redactionMarker)
		cursor = credentialValueEnd(value, valueStart, span)
		searchStart = cursor
		redacted = true
	}
	if !redacted {
		return value, false
	}
	result.WriteString(value[cursor:])
	return result.String(), true
}

func classifyCredentialKey(value string) credentialValueSpan {
	words := credentialKeyWords(value)
	if len(words) == 0 {
		return credentialValueNone
	}
	last := words[len(words)-1]
	if last == "authorization" {
		return credentialValueLine
	}
	switch last {
	case "token", "secret", "password", "passwd", "passphrase", "auth",
		"credential", "credentials", "apikey", "accesskey", "accesskeyid",
		"privatekey", "secretkey":
		return credentialValueToken
	case "key":
		if len(words) < 2 {
			return credentialValueNone
		}
		switch words[len(words)-2] {
		case "api", "access", "private", "secret":
			return credentialValueToken
		}
	case "id":
		if len(words) >= 3 &&
			words[len(words)-2] == "key" &&
			words[len(words)-3] == "access" {
			return credentialValueToken
		}
	}
	return credentialValueNone
}

func credentialKeyWords(value string) []string {
	words := make([]string, 0, 4)
	start := 0
	appendWord := func(end int) {
		if end > start {
			words = append(words, strings.ToLower(value[start:end]))
		}
	}
	for index := 0; index < len(value); index++ {
		current := value[index]
		if current == '_' || current == '-' {
			appendWord(index)
			start = index + 1
			continue
		}
		if index == start || !isASCIIUpper(current) {
			continue
		}
		previous := value[index-1]
		nextIsLower := index+1 < len(value) && isASCIILower(value[index+1])
		if isASCIILower(previous) || isASCIIDigit(previous) ||
			isASCIIUpper(previous) && nextIsLower {
			appendWord(index)
			start = index
		}
	}
	appendWord(len(value))
	return words
}

func credentialValueEnd(value string, start int, span credentialValueSpan) int {
	if value[start] == '"' || value[start] == '\'' {
		quote := value[start]
		for index := start + 1; index < len(value); index++ {
			if value[index] == quote && !isEscapedQuote(value, index) {
				return index + 1
			}
		}
		return len(value)
	}
	if span == credentialValueLine {
		if end := strings.IndexAny(value[start:], "\r\n"); end >= 0 {
			return start + end
		}
		return len(value)
	}
	for index := start; index < len(value); index++ {
		switch value[index] {
		case ' ', '\t', '\r', '\n', ',', ';':
			return index
		}
	}
	return len(value)
}

func isEscapedQuote(value string, index int) bool {
	backslashes := 0
	for index--; index >= 0 && value[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 != 0
}

func isASCIIUpper(value byte) bool { return value >= 'A' && value <= 'Z' }
func isASCIILower(value byte) bool { return value >= 'a' && value <= 'z' }
func isASCIIDigit(value byte) bool { return value >= '0' && value <= '9' }

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
