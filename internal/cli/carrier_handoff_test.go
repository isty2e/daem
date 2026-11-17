package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	observeantigravity "github.com/isty2e/daem/internal/assurance/observe/antigravityplugin"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/subprocess"
	applyworkflow "github.com/isty2e/daem/internal/workflow/apply"
	lockworkflow "github.com/isty2e/daem/internal/workflow/lock"
)

type handoffConfirmationReader func([]byte) (int, error)

func (read handoffConfirmationReader) Read(buffer []byte) (int, error) { return read(buffer) }

func TestSourceHandoffCLISeparatesConditionalPlanFromActualProgress(t *testing.T) {
	manifest := writeClaudeCLIRefreshFixture(t)
	var stdout, stderr bytes.Buffer
	seedExecutor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
		t.Fatal("state-only fixture adoption invoked a host command")
		return subprocess.CommandResult{MissingRunner: true}
	}})
	if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--yes", "--manage-existing"}, RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: seedExecutor}}); code != 0 {
		t.Fatalf("seed management: code=%d stderr=%s", code, stderr.String())
	}
	content, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "context7@market", "context7@next", 1))
	if err := os.WriteFile(manifest, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{ManifestPath: manifest}); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--dry-run", "--json"}, RunOptions{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("preview: code=%d stderr=%s", code, stderr.String())
	}
	var preview struct {
		Schema   int  `json:"schema_version"`
		Errors   bool `json:"has_errors"`
		Handoffs struct {
			Basis       string `json:"basis"`
			Retirements []struct {
				Before struct {
					Outcome string `json:"requested_outcome"`
				} `json:"before"`
				Settled *bool `json:"settled"`
			} `json:"retirements"`
			Acquisitions []struct {
				After struct {
					Source string `json:"source_ref"`
					State  string `json:"correlation_state"`
				} `json:"conditional_after"`
				Retirements []int `json:"requires_retirements"`
			} `json:"acquisitions"`
		} `json:"carrier_handoffs"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Schema != contractversion.ReconciliationPlanJSON || preview.Errors || preview.Handoffs.Basis != "plan_time_conditional_remainder" || len(preview.Handoffs.Retirements) != 1 || len(preview.Handoffs.Acquisitions) != 1 {
		t.Fatalf("bad handoff preview: %s", stdout.String())
	}
	if preview.Handoffs.Retirements[0].Before.Outcome != "replaced" || preview.Handoffs.Retirements[0].Settled != nil || preview.Handoffs.Acquisitions[0].After.Source != "context7@next" || preview.Handoffs.Acquisitions[0].After.State != "missing" || !slices.Equal(preview.Handoffs.Acquisitions[0].Retirements, []int{0}) {
		t.Fatal("preview lost replacement purpose, conditional observation or dependency")
	}

	stdout.Reset()
	stderr.Reset()
	calls := 0
	forbidden := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(context.Context, subprocess.CommandRequest) subprocess.CommandResult {
		calls++
		return subprocess.CommandResult{MissingRunner: true}
	}})
	answer := strings.NewReader("no\n")
	reader := handoffConfirmationReader(func(buffer []byte) (int, error) {
		if !strings.Contains(stdout.String(), "conditional after retirement") || !strings.Contains(stdout.String(), "context7@market") || !strings.Contains(stdout.String(), "context7@next") {
			t.Fatalf("confirmation preceded the old/conditional-new disclosure: stdout=%s stderr=%s", stdout.String(), stderr.String())
		}
		return answer.Read(buffer)
	})
	interactive := interactiveRunOptions(reader, &stdout, &stderr)
	interactive.ApplyExecuteOptions = applyworkflow.ExecuteOptions{HostRouteExecutor: forbidden}
	if code := RunWithOptions([]string{"apply", "--manifest", manifest}, interactive); code != 1 || calls != 0 {
		t.Fatal("declined handoff ran an effect")
	}

	inventory := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "plugins", "installed_plugins.json")
	failedOnce := false
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, command subprocess.CommandRequest) subprocess.CommandResult {
		calls++
		if !failedOnce {
			failedOnce = true
			return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 23}
		}
		data := `{"version":2,"plugins":{}}`
		if slices.Contains(command.Args, "context7@next") {
			data = fmt.Sprintf(`{"version":2,"plugins":{"context7@next":[{"scope":"project","projectPath":%q}]}}`, filepath.Dir(manifest))
		}
		if err := os.WriteFile(inventory, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
	}})
	for _, complete := range []bool{false, true} {
		stdout.Reset()
		stderr.Reset()
		code := RunWithOptions([]string{"apply", "--manifest", manifest, "--yes", "--json"}, RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}})
		var result struct {
			Schema   int  `json:"schema_version"`
			Errors   bool `json:"has_errors"`
			Handoffs struct {
				Requalified *bool `json:"requalified"`
				Complete    *bool `json:"apply_complete"`
				Retirements []struct {
					Settled *bool `json:"settled"`
				} `json:"retirements"`
			} `json:"carrier_handoffs"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("decode result: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
		}
		if result.Schema != contractversion.ApplyResultJSON || (code == 0) != complete || result.Errors == complete || result.Handoffs.Complete == nil || *result.Handoffs.Complete != complete || result.Handoffs.Requalified == nil || *result.Handoffs.Requalified != complete || len(result.Handoffs.Retirements) != 1 || result.Handoffs.Retirements[0].Settled == nil || *result.Handoffs.Retirements[0].Settled != complete {
			t.Fatalf("progress did not describe actual completion: %s stderr=%s", stdout.String(), stderr.String())
		}
	}
	if calls != 3 {
		t.Fatalf("host calls=%d, want failed remove, successful remove, install", calls)
	}
}

func TestHandoffPreviewDisclosesConditionalStateOnlyAcquisition(t *testing.T) {
	for _, test := range []struct {
		name                     string
		laterFailure, jsonOutput bool
	}{
		{"success-json", false, true}, {"partial-json", true, true}, {"partial-human", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := writeClaudeCLIRefreshFixture(t)
			content, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			inventory := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "plugins", "installed_plugins.json")
			writeInventory := func(old bool) {
				plugins := map[string]json.RawMessage{"retained@stable": json.RawMessage(fmt.Sprintf(`[{"scope":"project","projectPath":%q}]`, filepath.Dir(manifest)))}
				if old {
					plugins["context7@market"] = json.RawMessage(fmt.Sprintf(`[{"scope":"project","projectPath":%q}]`, filepath.Dir(manifest)))
				}
				data, err := json.Marshal(struct {
					Version int                        `json:"version"`
					Plugins map[string]json.RawMessage `json:"plugins"`
				}{2, plugins})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(inventory, data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			writeInventory(true)
			var stdout, stderr bytes.Buffer
			calls := 0
			executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, request subprocess.CommandRequest) subprocess.CommandResult {
				calls++
				if test.laterFailure && slices.Contains(request.Args, "install") && slices.Contains(request.Args, "later@market") {
					return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 23}
				}
				if !slices.Contains(request.Args, "uninstall") || !slices.Contains(request.Args, "context7@market") {
					t.Fatalf("handoff invoked an unexpected command: %s %v", request.Command, request.Args)
				}
				writeInventory(false)
				return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
			}})
			options := RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}}
			if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--yes", "--manage-existing"}, options); code != 0 || calls != 0 {
				t.Fatalf("seed claims: code=%d stderr=%s", code, stderr.String())
			}
			content = []byte(strings.Replace(string(content), "context7@market", "retained@stable", 1))
			if test.laterFailure {
				content = append(content, []byte("\n[[extension]]\nid='later'\ncarrier='claude-code-plugin'\nscope='project'\nsource={marketplace='later@market'}\n")...)
			}
			if err := os.WriteFile(manifest, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{ManifestPath: manifest}); err != nil {
				t.Fatal(err)
			}
			stdout.Reset()
			stderr.Reset()
			if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--dry-run", "--json", "--manage-existing"}, options); code != 0 {
				t.Fatalf("preview: code=%d stderr=%s", code, stderr.String())
			}
			var preview struct {
				Errors   bool `json:"has_errors"`
				Handoffs struct {
					Adoptions []struct {
						StateOnly      bool `json:"state_only"`
						ManageExisting bool `json:"manage_existing"`
						Eligible       bool `json:"lifecycle_eligible"`
					} `json:"conditional_carrier_adoption_actions"`
				} `json:"carrier_handoffs"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &preview); err != nil {
				t.Fatal(err)
			}
			if preview.Errors || len(preview.Handoffs.Adoptions) != 1 || !preview.Handoffs.Adoptions[0].StateOnly || !preview.Handoffs.Adoptions[0].ManageExisting || !preview.Handoffs.Adoptions[0].Eligible {
				t.Fatalf("conditional adoption was hidden: %s", stdout.String())
			}
			stdout.Reset()
			stderr.Reset()
			args := []string{"apply", "--manifest", manifest, "--yes", "--manage-existing"}
			if test.jsonOutput {
				args = append(args, "--json")
			}
			wantCode, wantCalls := 0, 1
			if test.laterFailure {
				wantCode, wantCalls = 1, 2
			}
			if code := RunWithOptions(args, options); code != wantCode || calls != wantCalls {
				t.Fatalf("state-only acquisition: code=%d calls=%d stderr=%s stdout=%s", code, calls, stderr.String(), stdout.String())
			}
			if test.jsonOutput {
				var receipt struct {
					Handoffs struct {
						Complete  bool `json:"apply_complete"`
						Adoptions []struct {
							Transition string `json:"claim_transition"`
							Provenance string `json:"final_claim_provenance"`
						} `json:"conditional_carrier_adoption_actions"`
					} `json:"carrier_handoffs"`
				}
				if err := json.Unmarshal(stdout.Bytes(), &receipt); err != nil {
					t.Fatal(err)
				}
				if receipt.Handoffs.Complete == test.laterFailure || len(receipt.Handoffs.Adoptions) != 1 || receipt.Handoffs.Adoptions[0].Transition != "recorded" || receipt.Handoffs.Adoptions[0].Provenance != "explicitly_adopted_observed" {
					t.Fatalf("actual adoption receipt was lost: %s", stdout.String())
				}
			} else if !strings.Contains(stderr.String(), "recorded external carrier claim") {
				t.Fatalf("partial result omitted completed adoption: %s", stderr.String())
			}
			state, err := statefile.Load(t.Context(), filepath.Join(filepath.Dir(manifest), ".daem", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			if len(state.ManagedCarrierClaims()) != 1 || state.ManagedCarrierClaims()[0].Identity().Carrier().Key().Source().Ref() != "retained@stable" {
				t.Fatal("handoff did not acquire the admitted existing carrier")
			}
		})
	}
}

func TestSuccessfulSameNameHandoffJSONDoesNotReportBeforeBlockAsFailure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	manifest := filepath.Join(root, "daem.toml")
	writeManifest := func(source string) {
		content := fmt.Sprintf("version=1\ntargets=['antigravity-cli']\n[[extension]]\nid='guidance'\ncarrier='antigravity-cli-plugin'\nscope='global'\nsource={host_source=%q}\n", source)
		if err := os.WriteFile(manifest, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := lockworkflow.RunLock(t.Context(), lockworkflow.LockInput{ManifestPath: manifest}); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := observeantigravity.ResolveHostPaths()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := paths.PluginDirectoryPath("guidance")
	if err != nil {
		t.Fatal(err)
	}
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{Runner: func(_ context.Context, command subprocess.CommandRequest) subprocess.CommandResult {
		content := `{"imports":[{"name":"guidance","source":"antigravity"}]}`
		if slices.Contains(command.Args, "uninstall") {
			content = `{"imports":[]}`
			if err := os.RemoveAll(bundle); err != nil {
				t.Fatal(err)
			}
		} else {
			if err := os.MkdirAll(bundle, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(bundle, "plugin.json"), []byte(`{"name":"guidance","version":"1.0.0"}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.MkdirAll(filepath.Dir(paths.ImportManifestPath()), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(paths.ImportManifestPath(), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return subprocess.CommandResult{Started: true, HasExitCode: true, ExitCode: 0}
	}})
	var stdout, stderr bytes.Buffer
	options := RunOptions{Stdout: &stdout, Stderr: &stderr, ApplyExecuteOptions: applyworkflow.ExecuteOptions{HostRouteExecutor: executor}}
	writeManifest("guidance@first")
	if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--yes"}, options); code != 0 {
		t.Fatalf("initial install: code=%d stderr=%s", code, stderr.String())
	}
	writeManifest("guidance@second")
	stdout.Reset()
	stderr.Reset()
	if code := RunWithOptions([]string{"apply", "--manifest", manifest, "--yes", "--json"}, options); code != 0 {
		t.Fatalf("handoff: code=%d stderr=%s", code, stderr.String())
	}
	var result struct {
		Errors    bool `json:"has_errors"`
		Relations []struct {
			Blocked bool `json:"blocks_ordinary_apply"`
		} `json:"relation_actions"`
		Handoffs struct {
			Complete     bool `json:"apply_complete"`
			Acquisitions []struct {
				After struct {
					Blocked bool `json:"blocks_ordinary_apply"`
				} `json:"conditional_after"`
			} `json:"acquisitions"`
		} `json:"carrier_handoffs"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Errors || !result.Handoffs.Complete || len(result.Relations) != 1 || !result.Relations[0].Blocked || len(result.Handoffs.Acquisitions) != 1 || result.Handoffs.Acquisitions[0].After.Blocked {
		t.Fatalf("successful conditional plan conflated before evidence with runtime failure: %s", stdout.String())
	}
}
