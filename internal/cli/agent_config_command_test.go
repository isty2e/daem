package cli

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/test/testkit/clijson"
)

func TestAgentConfigCommandSpellingsShareFailureIdentity(t *testing.T) {
	for _, verb := range []string{"add", "remove", "import", "unmanage", "replace"} {
		t.Run(verb, func(t *testing.T) {
			options := RunOptions{Context: t.Context()}
			preferred, stderr, code := runCLIEdge(t, []string{verb, "agent-config", "--json"}, options)
			if code != 2 || stderr != "" {
				t.Fatalf("preferred spelling: exit=%d stdout=%q stderr=%q", code, preferred, stderr)
			}
			clijson.DecodeFailure(t, preferred, verb+" host-config", "usage")

			legacy, stderr, code := runCLIEdge(t, []string{verb, "host-config", "--json"}, options)
			if code != 2 || stderr != "" || legacy != preferred {
				t.Fatalf("legacy spelling differs: exit=%d stdout=%q stderr=%q; preferred stdout=%q", code, legacy, stderr, preferred)
			}
		})
	}
}

func TestAgentConfigHelpUsesPreferredSpelling(t *testing.T) {
	for _, verb := range []string{"add", "remove", "import", "unmanage", "replace"} {
		t.Run(verb, func(t *testing.T) {
			options := RunOptions{Context: t.Context()}
			preferred, stderr, code := runCLIEdge(t, []string{"help", verb, "agent-config"}, options)
			if code != 0 || stderr != "" || !strings.HasPrefix(preferred, "daem "+verb+" agent-config - ") {
				t.Fatalf("preferred help: exit=%d stdout=%q stderr=%q", code, preferred, stderr)
			}

			aliases := [][]string{{"help", verb, "host-config"}, {verb, "host-config", "--help"}}
			if verb == "add" || verb == "remove" || verb == "unmanage" {
				aliases = append(aliases, []string{verb, "help", "host-config"})
			}
			for _, args := range aliases {
				legacy, stderr, code := runCLIEdge(t, args, options)
				if code != 0 || stderr != "" || legacy != preferred {
					t.Fatalf("%v did not show the preferred help: exit=%d stdout=%q stderr=%q", args, code, legacy, stderr)
				}
			}
		})
	}
}

func TestAgentConfigPathAliasesStayWithinTheirGrammar(t *testing.T) {
	for _, flag := range []string{"--config-path", "--host-path"} {
		t.Run(flag, func(t *testing.T) {
			options := RunOptions{Context: t.Context()}
			stdout, stderr, code := runCLIEdge(t, []string{"import", "agent-config", "prefs", "--update-source", flag + "=", "--dry-run", "--json"}, options)
			if code != 2 || stderr != "" {
				t.Fatalf("update with initial path flag: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
			}
			failure := clijson.DecodeFailure(t, stdout, "import host-config", "usage")
			if !strings.Contains(failure.Error.Message, "is initial-only") {
				t.Fatalf("expected the initial-only diagnostic for %s, got %+v", flag, failure.Error)
			}

			stdout, stderr, code = runCLIEdge(t, []string{"add", "agent-config", "prefs", "prefs.toml", flag, "--json"}, options)
			if code != 2 || stdout != "" || !strings.Contains(stderr, "requires an explicit --target and --scope") {
				t.Fatalf("%s consumed --json as an option instead of a path: exit=%d stdout=%q stderr=%q", flag, code, stdout, stderr)
			}
		})
	}
}
