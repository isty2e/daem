//go:build unix

package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/execute/executionevent"
	"github.com/isty2e/daem/internal/reconcile/carrierabsence"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestNativeProjectRetirementSurvivesProtectedHostRouteContinuation(t *testing.T) {
	storage := t.TempDir()
	t.Setenv("HOME", filepath.Join(storage, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(storage, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(storage, "state"))
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(storage, "pi"))
	t.Setenv("CODEX_HOME", filepath.Join(storage, "codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(storage, "claude"))
	root, manifestPath, _, _, previous, _ := writeApplyPiPackageCarrierCommandFixtureForScope(t, target.ScopeProject)
	retiring := seedApplyCarrierClaim(t, root, manifestPath, previous, target.ScopeProject)
	writeApplyFile(t, manifestPath, "version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\npath='settings.json'\nsource='./preferences.json'\nownership='keys'\n[[extension]]\nid='new-tools'\ncarrier='pi-package'\ntargets=['pi']\nscope='project'\nsource={host_source='git:github.com/acme/pi-new-tools@v1'}\n")
	writeApplyFile(t, filepath.Join(root, "preferences.json"), `{"theme":"native-retirement"}`)
	bin := filepath.Join(storage, "bin")
	script := "#!/bin/sh\nset -eu\nif [ \"$1\" = --version ]; then printf '1.0.2\\n'; exit 0; fi\n[ \"$1\" = install ] && [ \"$2\" = 'git:github.com/acme/pi-new-tools@v1' ] && [ \"$3\" = -l ] || exit 91\n[ -f .pi/settings.json ] || exit 92\nprintf '%s\\n' '{\"theme\":\"native-retirement\",\"packages\":[\"git:github.com/acme/pi-new-tools@v1\"]}' > .pi/settings.json\n"
	writeApplyFile(t, filepath.Join(bin, "pi"), script)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	input := CommandInput{ManifestPath: manifestPath}
	prepared, err := PlanWrite(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	absences := prepared.Reconciliation.CarrierAbsences()
	if len(absences) != 1 || absences[0].Decision() != carrierabsence.DecisionRetireAlreadyAbsent || !absences[0].Claim().ExactEqual(retiring) {
		t.Fatal("fixture did not select the exact already-absent project claim")
	}
	if prepared.lifecycle.planned.nativePlan == nil || !prepared.lifecycle.planned.nativePlan.opaque {
		t.Fatal("fixture did not reserve an opaque native continuation")
	}
	if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{PlanWasDisclosed: true, HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{})}); err != nil {
		t.Fatal(err)
	}

	retry, err := PlanWrite(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	state := retry.lifecycle.planned.assessment.CurrentState
	for _, action := range retry.Reconciliation.CarrierAbsences() {
		if action.Claim().ExactEqual(retiring) {
			t.Fatal("retired claim returned after the protected host-route continuation")
		}
	}
	if len(state.PendingCarrierRemovals()) != 0 || len(state.ManagedCarrierClaims()) != 1 || state.ManagedCarrierClaims()[0].ExactEqual(retiring) {
		t.Fatalf("protected continuation: absences=%d pending removals=%d claims=%#v pending installs=%#v", len(retry.Reconciliation.CarrierAbsences()), len(state.PendingCarrierRemovals()), state.ManagedCarrierClaims(), state.PendingCarrierInstalls())
	}
	content, err := os.ReadFile(filepath.Join(root, ".pi", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Theme    string   `json:"theme"`
		Packages []string `json:"packages"`
	}
	if err := json.Unmarshal(content, &document); err != nil || document.Theme != "native-retirement" || len(document.Packages) != 1 || document.Packages[0] != "git:github.com/acme/pi-new-tools@v1" {
		t.Fatalf("native settings and host registration did not survive: %s: %v", content, err)
	}
}

func TestNativeOrderSuffixUsesPostInstallReconciliation(t *testing.T) {
	storage := t.TempDir()
	for key, directory := range map[string]string{
		"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state",
		"PI_CODING_AGENT_DIR": "pi", "CODEX_HOME": "codex", "CLAUDE_CONFIG_DIR": "claude",
	} {
		t.Setenv(key, filepath.Join(storage, directory))
	}
	root, manifestPath, _, _, _, _ := writeApplyPiPackageCarrierCommandFixtureForScope(t, target.ScopeProject)
	writeApplyFile(t, manifestPath, "version=1\ntargets=['pi']\n[[host_config]]\nid='preferences'\npath='settings.json'\nsource='./preferences.json'\nownership='keys'\n[[extension]]\nid='alpha'\ncarrier='pi-package'\ntargets=['pi']\nscope='project'\nsource={host_source='npm:alpha@1'}\n[[extension]]\nid='beta'\ncarrier='pi-package'\ntargets=['pi']\nscope='project'\nsource={host_source='npm:beta@1'}\n")
	writeApplyFile(t, filepath.Join(root, "preferences.json"), `{"theme":"native-order"}`)
	bin := filepath.Join(storage, "bin")
	script := `#!/bin/sh
set -eu
if [ "$1" = --version ]; then printf '1.0.2\n'; exit 0; fi
[ "$1" = install ] && [ "$3" = -l ] || exit 91
case "$2" in
  npm:alpha@1) other='npm:beta@1' ;;
  npm:beta@1) other='npm:alpha@1' ;;
  *) exit 92 ;;
esac
if grep -F -q "$other" .pi/settings.json; then
  printf '%s\n' '{"theme":"native-order","packages":["npm:beta@1","npm:alpha@1"]}' > .pi/settings.json
else
  printf '{"theme":"native-order","packages":["%s"]}\n' "$2" > .pi/settings.json
fi
`
	writeApplyFile(t, filepath.Join(bin, "pi"), script)
	if err := os.Chmod(filepath.Join(bin, "pi"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: manifestPath}); err != nil {
		t.Fatal(err)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: manifestPath})
	if err != nil {
		t.Fatal(err)
	}
	started, done := 0, 0
	result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{
		PlanWasDisclosed: true, HostRouteExecutor: subprocess.NewCommandExecutor(subprocess.CommandOptions{}),
		ExecuteEvents: func(event executionevent.Event) {
			if event.Kind == executionevent.RelationOrderStarted {
				started++
			}
			if event.Kind == executionevent.RelationOrderDone {
				done++
			}
		},
	})
	if err != nil || len(result.RelationOrderResults) != 1 || result.RelationOrderResults[0].Outcome() != RelationOrderConverged ||
		!result.RelationOrderResults[0].Changed() || started != 1 || done != 1 {
		t.Fatal("published native order was reported from pre-install actions", result.RelationOrderResults, started, done, err)
	}
	var settings struct {
		Theme    string   `json:"theme"`
		Packages []string `json:"packages"`
	}
	content, err := os.ReadFile(filepath.Join(root, ".pi", "settings.json"))
	if err != nil || json.Unmarshal(content, &settings) != nil || settings.Theme != "native-order" ||
		len(settings.Packages) != 2 || settings.Packages[0] != "npm:alpha@1" || settings.Packages[1] != "npm:beta@1" {
		t.Fatal("native suffix did not publish the selected order after installation", string(content), err)
	}
}
