package mcp

import "github.com/isty2e/daem/internal/subprocess"

func sanitizeCapture(result commandResult, secrets []string, limit int) sanitizedCapture {
	if limit <= 0 {
		limit = defaultOutputLimit
	}
	policy := subprocess.NewCapturePolicy(secrets, limit)
	stdout := policy.Sanitize(result.Stdout, result.StdoutTruncated)
	stderr := policy.Sanitize(result.Stderr, result.StderrTruncated)

	var errorDetail subprocess.CaptureResult
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
