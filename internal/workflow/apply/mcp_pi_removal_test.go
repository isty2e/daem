package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/subprocess"
	workflowlock "github.com/isty2e/daem/internal/workflow/lock"
)

func TestPiProviderMCPRemovalRevalidatesConcurrentConfigEdits(t *testing.T) {
	t.Run("unowned sibling edit is preserved", func(t *testing.T) {
		root, manifestPath := writePiProviderMCPFixture(t)
		t.Setenv("HOME", filepath.Join(root, "home"))
		applyInitialPiProviderMCP(t, root, manifestPath)
		writePiProviderManifestWithoutMCP(t, manifestPath)
		if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
			ManifestPath: manifestPath,
		}); err != nil {
			t.Fatal(err)
		}
		prepared, err := PlanWrite(t.Context(), CommandInput{
			ManifestPath: manifestPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
		writeApplyFile(t, configPath, `{"mcpServers":{
			"context7":{
				"command":"node",
				"args":["server.js"],
				"env":{},
				"lifecycle":"lazy",
				"disabled":false
			},
			"concurrent":{"command":"keep"}
		}}`)

		if _, err := ExecuteWithOptions(
			t.Context(),
			prepared,
			ExecuteOptions{PlanWasDisclosed: true},
		); err != nil {
			t.Fatalf("ExecuteWithOptions returned error: %v", err)
		}
		content, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), `"context7"`) ||
			!strings.Contains(string(content), `"concurrent"`) {
			t.Fatalf("config after concurrent merge = %s", content)
		}
	})

	t.Run("managed entry edit rejects stale plan", func(t *testing.T) {
		root, manifestPath := writePiProviderMCPFixture(t)
		t.Setenv("HOME", filepath.Join(root, "home"))
		applyInitialPiProviderMCP(t, root, manifestPath)
		writePiProviderManifestWithoutMCP(t, manifestPath)
		if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
			ManifestPath: manifestPath,
		}); err != nil {
			t.Fatal(err)
		}
		prepared, err := PlanWrite(t.Context(), CommandInput{
			ManifestPath: manifestPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
		changed := `{"mcpServers":{"context7":{
			"command":"changed",
			"args":[],
			"env":{},
			"lifecycle":"lazy",
			"disabled":false
		}}}`
		writeApplyFile(t, configPath, changed)

		_, err = ExecuteWithOptions(
			t.Context(),
			prepared,
			ExecuteOptions{PlanWasDisclosed: true},
		)
		var stale mutation.StalePlanError
		if !errors.As(err, &stale) {
			t.Fatalf("ExecuteWithOptions error = %v, want StalePlanError", err)
		}
		content, readErr := os.ReadFile(configPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(content) != changed {
			t.Fatalf(
				"stale removal overwrote managed edit:\nwant=%s\ngot=%s",
				changed,
				content,
			)
		}
	})

	t.Run("effective fallback change rejects disclosed plan", func(t *testing.T) {
		root, manifestPath := writePiProviderMCPFixture(t)
		homeDir := filepath.Join(root, "home")
		t.Setenv("HOME", homeDir)
		applyInitialPiProviderMCP(t, root, manifestPath)
		writePiProviderManifestWithoutMCP(t, manifestPath)
		if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
			ManifestPath: manifestPath,
		}); err != nil {
			t.Fatal(err)
		}
		prepared, err := PlanWrite(t.Context(), CommandInput{
			ManifestPath: manifestPath,
		})
		if err != nil {
			t.Fatal(err)
		}
		selectedPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
		selectedBefore, err := os.ReadFile(selectedPath)
		if err != nil {
			t.Fatal(err)
		}
		fallbackPath := filepath.Join(homeDir, ".config", "mcp", "mcp.json")
		fallback := `{"mcpServers":{"context7":{
			"command":"fallback",
			"args":[],
			"env":{},
			"lifecycle":"lazy",
			"disabled":false
		}}}`
		writeApplyFile(t, fallbackPath, fallback)

		_, err = ExecuteWithOptions(
			t.Context(),
			prepared,
			ExecuteOptions{PlanWasDisclosed: true},
		)
		var stale mutation.StalePlanError
		if !errors.As(err, &stale) {
			t.Fatalf("ExecuteWithOptions error = %v, want StalePlanError", err)
		}
		selectedAfter, readErr := os.ReadFile(selectedPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(selectedAfter) != string(selectedBefore) {
			t.Fatalf(
				"stale fallback plan changed selected config:\nbefore=%s\nafter=%s",
				selectedBefore,
				selectedAfter,
			)
		}
		fallbackAfter, readErr := os.ReadFile(fallbackPath)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if string(fallbackAfter) != fallback {
			t.Fatalf("stale fallback plan changed unowned source: %s", fallbackAfter)
		}
	})
}

func TestPiProviderMCPExactAdoptionIsStateOnlyAndDriftIsFresh(t *testing.T) {
	root, manifestPath := writePiProviderMCPFixture(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	writePiProviderInstallation(t, root, "2.15.0")
	configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
	canonical := canonicalPiMCPEntry(t, "context7", "node", []string{"server.js"})
	existing := `{
		"mcpServers": {
			"context7": ` + string(canonical) + `,
			"manual": {"command": "keep"}
		},
		"settings": {"hostConfigDiscovery": "off"}
	}`
	writeApplyFile(t, configPath, existing)

	withoutManage, err := PlanDryRun(t.Context(), CommandInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("PlanDryRun without manage-existing returned error: %v", err)
	}
	decision := requireApplyMCPAggregateDecision(
		t,
		withoutManage.Reconciliation,
		"context7",
	)
	if decision.Kind() != reconcile.AggregateBlocked ||
		decision.Reason() != reconcile.ReasonUnmanagedOutputExists {
		t.Fatalf("unmanaged decision = %#v", decision)
	}

	withManage, err := PlanDryRun(t.Context(), CommandInput{
		ManifestPath:           manifestPath,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("PlanDryRun with manage-existing returned error: %v", err)
	}
	decision = requireApplyMCPAggregateDecision(
		t,
		withManage.Reconciliation,
		"context7",
	)
	if decision.Kind() != reconcile.AggregateRecord ||
		decision.Reason() != reconcile.ReasonManagedExisting ||
		decision.MutatesHost() {
		t.Fatalf("adoption decision = %#v, want state-only record", decision)
	}

	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath:           manifestPath,
		ManageUnmanagedMatches: true,
	})
	if err != nil {
		t.Fatalf("PlanWrite with manage-existing returned error: %v", err)
	}
	if _, err := ExecuteWithOptions(
		t.Context(),
		prepared,
		ExecuteOptions{},
	); err != nil {
		t.Fatalf("ExecuteWithOptions returned error: %v", err)
	}
	contentAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(contentAfter) != existing {
		t.Fatalf(
			"state-only adoption rewrote config:\nbefore=%s\nafter=%s",
			existing,
			contentAfter,
		)
	}
	statePath := filepath.Join(root, ".daem", "state.json")
	assertApplyMCPAggregateStateSubject(
		t,
		loadApplyStatefile(t, statePath),
		"context7",
		aggregate.MCPPlacementPiProject,
		canonical,
	)

	writeApplyFile(t, configPath, `{
		"mcpServers": {
			"context7": {
				"command": "changed",
				"args": [],
				"env": {},
				"lifecycle": "lazy",
				"disabled": false
			},
			"manual": {"command": "keep"}
		},
		"settings": {"hostConfigDiscovery": "off"}
	}`)
	drifted, err := PlanDryRun(t.Context(), CommandInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("PlanDryRun after drift returned error: %v", err)
	}
	decision = requireApplyMCPAggregateDecision(
		t,
		drifted.Reconciliation,
		"context7",
	)
	if decision.Kind() != reconcile.AggregateBlocked ||
		decision.Reason() != reconcile.ReasonDriftedOutput {
		t.Fatalf("drift decision = %#v", decision)
	}
}

func TestPiProviderMCPAdoptionRejectsLossyProviderConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "JSONC",
			content: `{
				// pi-mcp-adapter accepts this, but daem cannot rewrite it losslessly.
				"mcpServers": {
					"context7": {"command": "node", "args": ["server.js"]}
				}
			}`,
		},
		{
			name: "unsupported field",
			content: `{"mcpServers":{"context7":{
				"command":"node",
				"args":["server.js"],
				"providerFutureField":true
			}}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath := writePiProviderMCPFixture(t)
			t.Setenv("HOME", filepath.Join(root, "home"))
			writePiProviderInstallation(t, root, "2.15.0")
			configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
			writeApplyFile(t, configPath, test.content)

			plan, err := PlanDryRun(t.Context(), CommandInput{
				ManifestPath:           manifestPath,
				ManageUnmanagedMatches: true,
			})
			if err != nil {
				t.Fatalf("PlanDryRun returned error: %v", err)
			}
			decision := requireApplyMCPAggregateDecision(
				t,
				plan.Reconciliation,
				"context7",
			)
			if decision.Kind() != reconcile.AggregateBlocked ||
				decision.Reason() != reconcile.ReasonUnmanagedOutputExists {
				t.Fatalf("lossy adoption decision = %#v", decision)
			}
			contentAfter, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(contentAfter) != test.content {
				t.Fatalf(
					"blocked adoption rewrote config:\nbefore=%s\nafter=%s",
					test.content,
					contentAfter,
				)
			}
			if _, err := os.Stat(filepath.Join(root, ".daem", "state.json")); !os.IsNotExist(err) {
				t.Fatalf("statefile exists after blocked adoption: %v", err)
			}
		})
	}
}

func TestPiProviderMCPRemovalReportsAndPreservesUnownedFallbacks(t *testing.T) {
	tests := []struct {
		name           string
		fallback       string
		wantDetailPart string
	}{
		{
			name:           "no fallback",
			wantDetailPart: "no other same-name definition was observed",
		},
		{
			name: "equivalent fallback",
			fallback: `{"mcpServers":{"context7":{
				"command":"node",
				"args":["server.js"],
				"env":{},
				"lifecycle":"lazy",
				"disabled":false
			}}}`,
			wantDetailPart: "equivalent unowned lower-precedence",
		},
		{
			name: "materially different fallback",
			fallback: `{"mcpServers":{"context7":{
				"command":"different",
				"args":[],
				"env":{},
				"lifecycle":"lazy",
				"disabled":false
			}}}`,
			wantDetailPart: "materially different unowned lower-precedence",
		},
		{
			name: "incomparable fallback",
			fallback: `{"mcpServers":{"context7":{
				"command":"node",
				"args":["server.js"],
				"providerFutureField":true
			}}}`,
			wantDetailPart: "incomparable semantics",
		},
		{
			name:           "malformed fallback",
			fallback:       `{"mcpServers":`,
			wantDetailPart: "cannot be determined",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, manifestPath := writePiProviderMCPFixture(t)
			homeDir := filepath.Join(root, "home")
			t.Setenv("HOME", homeDir)
			applyInitialPiProviderMCP(t, root, manifestPath)

			configPath := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
			writeApplyFile(t, configPath, `{
				"mcpServers": {
					"context7": {
						"command": "node",
						"args": ["server.js"],
						"env": {},
						"lifecycle": "lazy",
						"disabled": false
					},
					"manual": {"command": "keep"}
				},
				"settings": {"hostConfigDiscovery": "off"}
			}`)
			fallbackPath := filepath.Join(homeDir, ".config", "mcp", "mcp.json")
			if test.fallback != "" {
				writeApplyFile(t, fallbackPath, test.fallback)
			}
			var fallbackBefore []byte
			if test.fallback != "" {
				var err error
				fallbackBefore, err = os.ReadFile(fallbackPath)
				if err != nil {
					t.Fatal(err)
				}
			}
			settingsPath := filepath.Join(root, ".pi", "settings.json")
			settingsBefore, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatal(err)
			}

			writePiProviderManifestWithoutMCP(t, manifestPath)
			if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
				ManifestPath: manifestPath,
			}); err != nil {
				t.Fatalf("RunLock returned error: %v", err)
			}

			dryRun, err := PlanDryRun(t.Context(), CommandInput{
				ManifestPath: manifestPath,
			})
			if err != nil {
				t.Fatalf("PlanDryRun returned error: %v", err)
			}
			decision := requireApplyMCPAggregateDecision(
				t,
				dryRun.Reconciliation,
				"context7",
			)
			if decision.Kind() != reconcile.AggregateRemove ||
				decision.Reason() != reconcile.ReasonRemovedFromManifest ||
				!strings.Contains(decision.Detail(), test.wantDetailPart) ||
				!strings.Contains(decision.Detail(), "provider package removal is separate") ||
				!strings.Contains(decision.Detail(), "runtime absence is not claimed") {
				t.Fatalf(
					"removal decision = kind %q reason %q detail %q",
					decision.Kind(),
					decision.Reason(),
					decision.Detail(),
				)
			}
			if test.fallback != "" &&
				!strings.Contains(decision.Detail(), fallbackPath) {
				t.Fatalf(
					"removal detail %q does not identify fallback %q",
					decision.Detail(),
					fallbackPath,
				)
			}

			prepared, err := PlanWrite(t.Context(), CommandInput{
				ManifestPath: manifestPath,
			})
			if err != nil {
				t.Fatalf("PlanWrite returned error: %v", err)
			}
			if _, err := ExecuteWithOptions(
				t.Context(),
				prepared,
				ExecuteOptions{},
			); err != nil {
				t.Fatalf("ExecuteWithOptions returned error: %v", err)
			}

			configAfter, err := os.ReadFile(configPath)
			if err != nil {
				t.Fatalf("read selected config after removal: %v", err)
			}
			if strings.Contains(string(configAfter), `"context7"`) ||
				!strings.Contains(string(configAfter), `"manual"`) ||
				!strings.Contains(string(configAfter), `"hostConfigDiscovery"`) {
				t.Fatalf("selected config after removal = %s", configAfter)
			}
			info, err := os.Stat(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != aggregate.DocumentFileMode {
				t.Fatalf(
					"selected config mode = %04o, want %04o",
					info.Mode().Perm(),
					aggregate.DocumentFileMode,
				)
			}
			if test.fallback != "" {
				fallbackAfter, err := os.ReadFile(fallbackPath)
				if err != nil {
					t.Fatal(err)
				}
				if string(fallbackAfter) != string(fallbackBefore) {
					t.Fatalf(
						"unowned fallback changed:\nbefore=%s\nafter=%s",
						fallbackBefore,
						fallbackAfter,
					)
				}
			}
			settingsAfter, err := os.ReadFile(settingsPath)
			if err != nil {
				t.Fatal(err)
			}
			if string(settingsAfter) != string(settingsBefore) {
				t.Fatalf(
					"provider settings changed:\nbefore=%s\nafter=%s",
					settingsBefore,
					settingsAfter,
				)
			}
			if _, err := os.Stat(
				filepath.Join(piProviderPackagePath(root), "package.json"),
			); err != nil {
				t.Fatalf("provider package removed with MCP contribution: %v", err)
			}
			assertPiMCPStateSubjectMissing(
				t,
				loadApplyStatefile(t, filepath.Join(root, ".daem", "state.json")),
				"context7",
			)
		})
	}
}

func TestPiProviderMCPRemovalRetainsGlobalProviderAndConsumer(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	agentRoot := filepath.Join(root, "pi-agent")
	manifestPath := filepath.Join(root, "daem.toml")
	t.Setenv("HOME", homeDir)
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	writeApplyFile(t, manifestPath, piSharedProviderManifest(true))
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("initial RunLock returned error: %v", err)
	}
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(
			_ context.Context,
			_ subprocess.CommandRequest,
		) subprocess.CommandResult {
			writeApplyFile(
				t,
				filepath.Join(agentRoot, "settings.json"),
				`{"packages":["`+piProviderSource+`"]}`,
			)
			writeApplyFile(
				t,
				filepath.Join(
					agentRoot,
					"npm",
					"node_modules",
					"pi-mcp-adapter",
					"package.json",
				),
				`{"name":"pi-mcp-adapter","version":"2.15.0"}`,
			)
			return subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 0,
			}
		},
	})
	initial, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("initial PlanWrite returned error: %v", err)
	}
	if _, err := ExecuteWithOptions(t.Context(), initial, ExecuteOptions{
		HostRouteExecutor: executor,
	}); err != nil {
		t.Fatalf("initial ExecuteWithOptions returned error: %v", err)
	}

	writeApplyFile(t, manifestPath, piSharedProviderManifest(false))
	if _, err := workflowlock.RunLock(t.Context(), workflowlock.LockInput{
		ManifestPath: manifestPath,
	}); err != nil {
		t.Fatalf("removal RunLock returned error: %v", err)
	}
	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("removal PlanWrite returned error: %v", err)
	}
	decision := requireApplyMCPAggregateDecision(
		t,
		prepared.Reconciliation,
		"project-server",
	)
	if decision.Kind() != reconcile.AggregateRemove ||
		!strings.Contains(decision.Detail(), "provider package removal is separate") {
		t.Fatalf("project removal decision = %#v", decision)
	}
	result, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{})
	if err != nil {
		t.Fatalf("removal ExecuteWithOptions returned error: %v", err)
	}
	if len(result.HostRouteAttempts) != 0 {
		t.Fatalf(
			"project MCP removal invoked provider route: %#v",
			result.HostRouteAttempts,
		)
	}

	projectConfig := filepath.Join(root, aggregate.PiProjectMCPConfigPath)
	if content, readErr := os.ReadFile(projectConfig); readErr == nil &&
		strings.Contains(string(content), `"project-server"`) {
		t.Fatalf("project MCP entry remains after removal: %s", content)
	} else if readErr != nil && !os.IsNotExist(readErr) {
		t.Fatalf("read project MCP config after removal: %v", readErr)
	}
	globalConfig, err := os.ReadFile(filepath.Join(agentRoot, "mcp.json"))
	if err != nil {
		t.Fatalf("read global MCP config after project removal: %v", err)
	}
	if !strings.Contains(string(globalConfig), `"global-server"`) {
		t.Fatalf("global MCP consumer was removed: %s", globalConfig)
	}
	if _, err := os.Stat(filepath.Join(
		agentRoot,
		"npm",
		"node_modules",
		"pi-mcp-adapter",
		"package.json",
	)); err != nil {
		t.Fatalf("shared global provider was removed: %v", err)
	}
}

func piSharedProviderManifest(includeProject bool) string {
	project := ""
	if includeProject {
		project = `
[[mcp_server]]
name = "project-server"
targets = ["pi"]
scope = "project"
transport = "stdio"
command = "node"
args = ["project.js"]
`
	}
	return `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-global"
carrier = "pi-package"
targets = ["pi"]
scope = "global"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }
` + project + `
[[mcp_server]]
name = "global-server"
targets = ["pi"]
scope = "global"
transport = "stdio"
command = "node"
args = ["global.js"]
`
}

func canonicalPiMCPEntry(
	t *testing.T,
	serverID string,
	command string,
	args []string,
) []byte {
	t.Helper()
	canonical, err := mcpcodec.CanonicalPiMCPAdapterServerEntry(
		mcpcodec.PiMCPAdapterServerProjection{
			ServerID:        serverID,
			Command:         command,
			Args:            args,
			Env:             map[string]string{},
			AdapterContract: aggregate.PiMCPAdapterStdioV1,
		},
	)
	if err != nil {
		t.Fatalf("CanonicalPiMCPAdapterServerEntry returned error: %v", err)
	}
	return canonical
}

func applyInitialPiProviderMCP(
	t *testing.T,
	root string,
	manifestPath string,
) {
	t.Helper()
	executor := subprocess.NewCommandExecutor(subprocess.CommandOptions{
		Runner: func(
			_ context.Context,
			_ subprocess.CommandRequest,
		) subprocess.CommandResult {
			writePiProviderInstallation(t, root, "2.15.0")
			return subprocess.CommandResult{
				Started: true, HasExitCode: true, ExitCode: 0,
			}
		},
	})
	prepared, err := PlanWrite(t.Context(), CommandInput{
		ManifestPath: manifestPath,
	})
	if err != nil {
		t.Fatalf("initial PlanWrite returned error: %v", err)
	}
	if _, err := ExecuteWithOptions(t.Context(), prepared, ExecuteOptions{
		HostRouteExecutor: executor,
	}); err != nil {
		t.Fatalf("initial ExecuteWithOptions returned error: %v", err)
	}
}

func writePiProviderManifestWithoutMCP(t *testing.T, manifestPath string) {
	t.Helper()
	writeApplyFile(t, manifestPath, `version = 1
targets = ["pi"]

[[extension]]
id = "pi-mcp-adapter-project"
carrier = "pi-package"
targets = ["pi"]
scope = "project"
source = { host_source = "npm:pi-mcp-adapter@^2.13.0" }
`)
}

func assertPiMCPStateSubjectMissing(
	t *testing.T,
	snapshot durable.Snapshot,
	serverID string,
) {
	t.Helper()
	for _, state := range snapshot.ManagedAggregates() {
		placement, admitted := aggregate.MCPPlacementForSubject(state.Subject())
		if admitted &&
			placement.ID() == aggregate.MCPPlacementPiProject &&
			state.Subject().Key() == serverID {
			t.Fatalf(
				"Pi MCP subject %q unexpectedly remains in state: %#v",
				serverID,
				state,
			)
		}
	}
}
