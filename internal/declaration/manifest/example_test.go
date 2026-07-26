package manifest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/target"
)

func TestExampleManifestParses(t *testing.T) {
	path := filepath.Join("..", "..", "..", "examples", "daem.toml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	environment, err := Decode(content)
	if err != nil {
		t.Fatalf("Bytes returned error: %v", err)
	}

	targets := environment.Targets()
	if len(targets) != 2 || targets[0] != target.TargetCodex || targets[1] != target.TargetClaudeCode {
		t.Fatalf("Targets = %#v, want codex and claude-code", targets)
	}
	if len(environment.Instructions()) != 1 {
		t.Fatalf("len(Instructions) = %d, want 1", len(environment.Instructions()))
	}
	if len(environment.Skills()) != 0 {
		t.Fatalf("Skills = %#v, want optional skill example to stay commented", environment.Skills())
	}
	if len(environment.Hooks()) != 0 {
		t.Fatalf("Hooks = %#v, want no first-run hooks", environment.Hooks())
	}
}
