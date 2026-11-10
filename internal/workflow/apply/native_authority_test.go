package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeAuthorityPlanningRetainsItsQualifiedRouteAfterPreview(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, directory := range map[string]string{
		"HOME": "home", "XDG_DATA_HOME": "data", "XDG_STATE_HOME": "state",
		"XDG_CONFIG_HOME": "config", "PI_CODING_AGENT_DIR": "agent-a", "CODEX_HOME": "codex",
	} {
		t.Setenv(name, filepath.Join(base, directory))
	}
	workspace, native, ordinary := filepath.Join(base, "workspace"), filepath.Join(base, "agent-a"), filepath.Join(base, "agent-b")
	for _, directory := range []string{workspace, native, ordinary} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(workspace, "daem.toml")
	for name, content := range map[string]string{
		"daem.toml":        fmt.Sprintf("version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\nscope='global'\npath='settings.json'\nsource=%q\nownership='keys'\n", filepath.Join(workspace, "preferences.json")),
		"preferences.json": `{"theme":"selected"}`,
	} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifest})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := prepared.Close(); err != nil {
			t.Error(err)
		}
	})
	planned := prepared.lifecycle.planned
	if planned.nativeOperation != nil {
		t.Fatal("fixture did not use the resource-free preview")
	}

	t.Setenv("PI_CODING_AGENT_DIR", ordinary)
	evidence, err := buildApplyAuthorityEvidence(t.Context(), planned)
	if err != nil {
		t.Fatal(err)
	}
	want := mutation.PhysicalAuthorityRequest{Path: filepath.Join(native, "settings.json"), Target: string(target.TargetPi), Scope: string(target.ScopeGlobal)}
	if !slices.Contains(evidence.documentWriteRequests, want) {
		t.Fatal("preview authority was replaced by a fresh ordinary selector", evidence.documentWriteRequests)
	}
	for _, request := range evidence.documentWriteRequests {
		if request.Path == filepath.Join(ordinary, "settings.json") {
			t.Fatal("native planning acquired the unrelated ordinary document")
		}
	}

	destination, err := output.Parse("@pi-agent/unselected.json")
	if err != nil {
		t.Fatal(err)
	}
	unselected, err := aggregate.NewDocumentAddress(target.TargetPi, target.ScopeGlobal, destination)
	if err != nil {
		t.Fatal(err)
	}
	if request, err := nativeAuthorityRequestFor(planned, unselected); err == nil || request.Path != "" {
		t.Fatal("an unselected native alias fell back to ordinary resolution")
	}
	for _, directory := range []string{native, ordinary} {
		if _, err := os.Stat(filepath.Join(directory, "settings.json")); !os.IsNotExist(err) {
			t.Fatal("authority planning created a host document", err)
		}
	}
}
