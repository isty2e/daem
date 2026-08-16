package listworkflow

import (
	"reflect"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/target"
)

func TestAvailableTargetsUnionsHeaderAndListableResourceFamilies(t *testing.T) {
	environment, err := declarationmanifest.Decode([]byte(`
version = 1
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["opencode"]

[[skill_group]]
names = ["alpha"]
source = { path = "skills", mode = "vendor" }
targets = ["antigravity-cli"]

[[hook]]
name = "prime-session"
event = "SessionStart"
command = "bd prime"
targets = ["opencode"]

[instructions.project]
source = "AGENTS.md"
targets = ["codex"]

[[mcp_server]]
name = "repo-tools"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "repo-tools"

[[extension]]
id = "context7"
carrier = "claude-code-plugin"
targets = ["claude-code"]
scope = "project"
source = { marketplace = "context7@market" }
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}

	got := availableTargets(environment)
	want := []target.Target{
		target.TargetCodex,
		target.TargetClaudeCode,
		target.TargetOpenCode,
		target.TargetPi,
		target.TargetAntigravityCLI,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availableTargets = %#v, want %#v", got, want)
	}
}

func TestAvailableTargetsKeepsHeaderWhenResourcesAreEmpty(t *testing.T) {
	environment, err := declarationmanifest.Decode([]byte(`
version = 1
targets = ["codex"]
`))
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	got := availableTargets(environment)
	want := []target.Target{target.TargetCodex}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("availableTargets = %#v, want %#v", got, want)
	}
}
