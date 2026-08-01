package ownership

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/pathauthority/pathtest"
	"github.com/isty2e/daem/internal/output"
)

func TestProvisionalAcquireIntentRequiresGlobalDestination(t *testing.T) {
	root := t.TempDir()
	namespace := filepath.Join(root, "skills")
	candidate := filepath.Join(namespace, "Caf\u00e9")
	provisional, err := pathauthority.NewProvisional(
		candidate,
		pathtest.DarwinCaseSensitive(candidate).Witness(),
		namespace,
		pathtest.DarwinCaseSensitive(namespace).Witness(),
	)
	if err != nil {
		t.Fatal(err)
	}
	projectDestination, err := output.Parse("skills/Caf\u00e9")
	if err != nil {
		t.Fatal(err)
	}
	authority := mustAuthority(
		t,
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)

	if _, err := NewProvisionalAcquireIntent(
		projectDestination,
		"",
		provisional,
		authority,
		"operation-1",
	); err == nil {
		t.Fatal("provisional ownership acquisition accepted a project destination")
	}
}
