//go:build darwin || linux

package apply

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/pelletier/go-toml/v2"

	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/metadataconversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/recoverygate"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestApplyConvertsEligibleMetadataBeforeItsExistingReplan(t *testing.T) {
	for _, test := range []struct {
		name, scope string
		native      bool
		foreignBad  bool
	}{
		{name: "ordinary project", scope: "project"},
		{name: "native project", scope: "project", native: true},
		{name: "global conversion or safe deferral", scope: "global"},
		{name: "foreign conversion failure does not block", scope: "project", foreignBad: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for key, directory := range map[string]string{
				"HOME": "home", "XDG_CONFIG_HOME": "config", "XDG_STATE_HOME": "state", "XDG_DATA_HOME": "data",
				"PI_CODING_AGENT_DIR": "pi", "CODEX_HOME": "codex", "CLAUDE_CONFIG_DIR": "claude",
			} {
				path := filepath.Join(root, directory)
				t.Setenv(key, path)
				if err := os.MkdirAll(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			workspace := filepath.Join(root, "workspace")
			if err := os.Mkdir(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			paths := applyTestPaths(t, workspace)
			source := filepath.Join(workspace, "instructions.md")
			before, after := "initial declaration\n", "updated declaration\n"
			output := filepath.Join(workspace, "AGENTS.md")
			manifest := fmt.Sprintf("version=1\ntargets=['codex']\n[instructions.%s]\nsource=%q\n", test.scope, source)
			if test.scope == "global" {
				output = filepath.Join(os.Getenv("HOME"), ".codex", "AGENTS.md")
			}
			if test.native {
				source = filepath.Join(workspace, "preferences.toml")
				before, after = "[tui]\nnotifications=false\n", "[tui]\nnotifications=true\n"
				output = filepath.Join(workspace, ".codex", "config.toml")
				manifest = fmt.Sprintf("version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='project'\npath='config.toml'\nsource=%q\nownership='keys'\n", source)
			}
			writeApplyFile(t, paths.ManifestPath, manifest)
			writeApplyFile(t, source, before)
			var foreignState string
			if test.foreignBad {
				foreign := filepath.Join(root, "foreign")
				foreignManifest := filepath.Join(foreign, "daem.toml")
				foreignSource := filepath.Join(foreign, "preferences.toml")
				writeApplyFile(t, foreignManifest, fmt.Sprintf("version=1\ntargets=['codex']\n[[host_config]]\nid='preferences'\nscope='project'\npath='config.toml'\nsource=%q\nownership='keys'\n", foreignSource))
				writeApplyFile(t, foreignSource, "[tui]\nnotifications=false\n")
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: foreignManifest}); err != nil {
					t.Fatal(err)
				}
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: foreignManifest})
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()
				if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{}); err != nil {
					t.Fatal(err)
				}
				foreignState = filepath.Join(foreign, ".daem", "state.json")
				writeApplyFile(t, foreignState, "{broken")
			}
			lockAndExecute := func(writes bool) {
				t.Helper()
				if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{ManifestPath: paths.ManifestPath}); err != nil {
					t.Fatal(err)
				}
				prepared, err := PlanWrite(t.Context(), CommandInput{ManifestPath: paths.ManifestPath})
				if err != nil {
					t.Fatal(err)
				}
				defer prepared.Close()
				planned := prepared.lifecycle.planned
				providers, err := providerInstallActions(planned.assessment.MCPProviders)
				if err != nil {
					t.Fatal(err)
				}
				envelope, err := metadataMaintenanceEnvelopeFor(planned, providers)
				if err != nil {
					t.Fatalf("pre-lease compiler could not classify planned work: writes=%t native-plan=%t native-operation=%t: %v", writes, planned.nativePlan != nil, planned.nativeOperation != nil, err)
				}
				if (envelope.Demand().EnsureCalls() != 0) != writes {
					t.Fatalf("pre-lease forward work did not match the fixture operation: ensures=%d writes=%t", envelope.Demand().EnsureCalls(), writes)
				}
				if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{PlanWasDisclosed: true}); err != nil {
					t.Fatal(err)
				}
			}
			lockAndExecute(true)
			legacy, err := os.ReadFile(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			var version struct{ Version int }
			if err := json.Unmarshal(legacy, &version); err != nil || version.Version != 10 {
				t.Fatal("fixture did not begin with legacy State", err, version)
			}
			identity, err := os.Stat(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			lockAndExecute(false)
			unchanged, err := os.ReadFile(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			currentIdentity, err := os.Stat(paths.StatefilePath)
			if err != nil || !bytes.Equal(legacy, unchanged) || !os.SameFile(identity, currentIdentity) || !identity.ModTime().Equal(currentIdentity.ModTime()) {
				t.Fatal("no-op apply rewrote State or bootstrapped metadata conversion", err)
			}
			if _, err := os.Stat(paths.GlobalMetadataDir()); !os.IsNotExist(err) {
				t.Fatal("no-op apply created conversion storage", err)
			}
			writeApplyFile(t, source, after)
			authority, err := recoverygate.NewEffectAuthority(t.Context(), paths)
			if err != nil {
				t.Fatal(err)
			}
			budget, err := physicalwork.NewBudget(0)
			if err != nil {
				t.Fatal(err)
			}
			_, qualificationErr := metadataconversion.Prepare(t.Context(), authority, budget, nil)
			if test.foreignBad && qualificationErr == nil {
				t.Fatal("malformed foreign State did not defer conversion")
			}
			if !test.foreignBad && qualificationErr != nil && !errors.Is(qualificationErr, mutation.ErrLegacyReferenceUnprovable) {
				t.Fatal("fixture conversion failed for an unexpected reason", qualificationErr)
			}
			lockAndExecute(true)
			content, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			if test.native {
				var preferences struct {
					TUI struct{ Notifications bool }
				}
				if err := toml.Unmarshal(content, &preferences); err != nil || !preferences.TUI.Notifications {
					t.Fatal("normal native preference write did not complete", err)
				}
			} else if !bytes.Equal(content, []byte(after)) {
				t.Fatal("normal declared write did not complete")
			}
			content, err = os.ReadFile(paths.StatefilePath)
			if err != nil {
				t.Fatal(err)
			}
			if qualificationErr == nil {
				if _, err := statefile.UnmarshalImage(content); err != nil {
					t.Fatal("eligible write did not consume a converted State image", err)
				}
			} else if err := json.Unmarshal(content, &version); err != nil || version.Version != 10 {
				t.Fatal("deferred conversion did not preserve the usable legacy format", err, version)
			}
			if foreignState != "" {
				content, err := os.ReadFile(foreignState)
				if err != nil || string(content) != "{broken" {
					t.Fatal("normal write changed the foreign State", err)
				}
			}
		})
	}
}
