package diagnose

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/findings"
)

func TestGitCheckClassifiesSuccessAndFailuresWithoutCallingThemUnavailable(t *testing.T) {
	cases := []struct {
		name       string
		version    func(context.Context) (string, error)
		want       findings.Severity
		wantDetail string
	}{
		{
			name: "success",
			version: func(context.Context) (string, error) {
				return "git version test", nil
			},
			want:       findings.SeverityOK,
			wantDetail: "git version test",
		},
		{
			name: "missing executable",
			version: func(context.Context) (string, error) {
				return "", fmt.Errorf("locate: %w", exec.ErrNotFound)
			},
			want:       findings.SeverityError,
			wantDetail: "git executable was not found in PATH",
		},
		{
			name: "command failure",
			version: func(context.Context) (string, error) {
				return "", errors.New("exit status 7")
			},
			want:       findings.SeverityError,
			wantDetail: "git --version failed: exit status 7",
		},
		{
			name: "empty output",
			version: func(context.Context) (string, error) {
				return "", nil
			},
			want:       findings.SeverityError,
			wantDetail: "git --version returned empty output",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			check := gitCheckWithTimeout(context.Background(), time.Second, tc.version)
			if check.Name != "git" || check.Severity != tc.want || check.Detail != tc.wantDetail {
				t.Fatalf("check = %#v, want name=git severity=%s detail=%q", check, tc.want, tc.wantDetail)
			}
			if strings.Contains(check.Detail, "git is unavailable") {
				t.Fatalf("check retained ambiguous unavailable classification: %#v", check)
			}
		})
	}
}

func TestGitCheckSeparatesOwnTimeoutFromCallerCancellation(t *testing.T) {
	waitForCancellation := func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}

	t.Run("check timeout", func(t *testing.T) {
		check := gitCheckWithTimeout(context.Background(), 10*time.Millisecond, waitForCancellation)
		if check.Severity != findings.SeverityError ||
			check.Detail != "git version check timed out after 10ms" {
			t.Fatalf("check = %#v, want internal timeout", check)
		}
	})

	t.Run("caller cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		check := gitCheckWithTimeout(ctx, time.Second, waitForCancellation)
		if check.Severity != findings.SeverityError ||
			check.Detail != "git check stopped by caller context: context canceled" {
			t.Fatalf("check = %#v, want caller cancellation", check)
		}
		if strings.Contains(check.Detail, "timed out") {
			t.Fatalf("caller cancellation was mislabeled as timeout: %#v", check)
		}
	})
}

func TestGitObjectFormatCapabilityLabel(t *testing.T) {
	t.Parallel()

	if got := gitObjectFormatCapabilityLabel("usage: git init [--object-format=<format>]"); got != "object-format sha1,sha256" {
		t.Fatalf("capable git label = %q", got)
	}
	if got := gitObjectFormatCapabilityLabel("usage: git init"); got != "object-format sha1" {
		t.Fatalf("legacy git label = %q", got)
	}
}
