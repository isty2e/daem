package cli

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestAgentConfigFailureIdentityNamesTheInvokedLeaf(t *testing.T) {
	for _, verb := range []string{"add", "remove", "import", "unmanage", "replace"} {
		t.Run(verb, func(t *testing.T) {
			options := RunOptions{Context: t.Context()}
			stdout, stderr, code := runCLIEdge(t, []string{verb, "agent-config", "--json"}, options)
			if code != 2 || stderr != "" {
				t.Fatalf("missing declaration ID: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			clijson.DecodeFailure(t, stdout, verb+" agent-config", "usage")
		})
	}
}

func TestAgentConfigPathStaysWithinItsGrammar(t *testing.T) {
	options := RunOptions{Context: t.Context()}
	stdout, stderr, code := runCLIEdge(t, []string{"import", "agent-config", "prefs", "--update-source", "--config-path=", "--dry-run", "--json"}, options)
	if code != 2 || stderr != "" {
		t.Fatalf("update with initial path flag: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	failure := clijson.DecodeFailure(t, stdout, "import agent-config", "usage")
	if !strings.Contains(failure.Error.Message, "is initial-only") {
		t.Fatalf("expected the initial-only diagnostic for --config-path, got %+v", failure.Error)
	}

	stdout, stderr, code = runCLIEdge(t, []string{"add", "agent-config", "prefs", "prefs.toml", "--config-path", "--json"}, options)
	if code != 2 || stdout != "" || !strings.Contains(stderr, "requires an explicit --target and --scope") {
		t.Fatalf("--config-path did not consume --json as a path: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestAgentConfigRejectsUnreleasedCommandAndFlagNames(t *testing.T) {
	options := RunOptions{Context: t.Context()}
	for _, verb := range []string{"add", "remove", "import", "unmanage", "replace"} {
		t.Run(verb, func(t *testing.T) {
			stdout, stderr, code := runCLIEdge(t, []string{verb, "host-config", "--json"}, options)
			if code != 2 {
				t.Fatalf("old subject: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			if verb == "import" {
				clijson.DecodeFailure(t, stdout, "import", "usage")
			} else if stdout != "" || stderr == "" {
				t.Fatalf("old subject selected a JSON leaf: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}

	stdout, stderr, code := runCLIEdge(t, []string{
		"add", "agent-config", "prefs", "prefs.toml", "--target", "codex", "--scope", "project",
		"--host-path=config.toml", "--json",
	}, options)
	if code != 2 || stderr != "" {
		t.Fatalf("old destination flag: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	clijson.DecodeFailure(t, stdout, "add agent-config", "usage")
}
