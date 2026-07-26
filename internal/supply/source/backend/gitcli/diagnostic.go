package gitcli

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode"

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
	gitBearerPattern = regexp.MustCompile(`(?i)(\bbearer\s+)[A-Za-z0-9._~+/=-]+`)
)

func gitCommandErrorWithCapture(err error, stderr string, truncated bool) error {
	if stderrText := sanitizeGitDiagnosticCapture(stderr, truncated); stderrText != "" {
		return fmt.Errorf("%s", stderrText)
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		if stderrText := sanitizeGitDiagnostic(string(exitError.Stderr)); stderrText != "" {
			return fmt.Errorf("%s", stderrText)
		}
	}

	return err
}

func sanitizeGitDiagnostic(value string) string {
	return sanitizeGitDiagnosticCapture(value, false)
}

func sanitizeGitDiagnosticCapture(value string, truncated bool) string {
	withoutFormatCharacters := strings.Map(func(character rune) rune {
		if unicode.In(character, unicode.Cf) {
			return ' '
		}
		return character
	}, value)
	redacted := gitURLUserinfoPattern.ReplaceAllString(withoutFormatCharacters, `${1}<redacted>@`)
	redacted = gitQuerySecretPattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	redacted = gitAuthorizationPattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	redacted = gitBearerPattern.ReplaceAllString(redacted, `${1}[REDACTED]`)
	result := subprocess.NewCapturePolicy(nil, maxGitDiagnosticRunes).Sanitize(redacted, truncated)
	return strings.Join(strings.Fields(result.Text()), " ")
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
