//go:build darwin || linux

package diagnose

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/findings"
)

func TestGitEnvironmentCheckUsesNativeExecutor(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("native Git is unavailable: %v", err)
	}

	check := gitCheck(t.Context())
	if check.Name != "git" || check.Status != findings.CheckOK ||
		!strings.HasPrefix(check.Detail, "git version ") ||
		!strings.Contains(check.Detail, "; object-format sha1") {
		t.Fatalf("check = %#v, want native Git version and object-format capability", check)
	}
}
