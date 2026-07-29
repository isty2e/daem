package host

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestObservePiAdapterCoversSixOrderedNormalLayers(t *testing.T) {
	homeDir := filepath.Join(t.TempDir(), "home")
	workDir := filepath.Join(t.TempDir(), "project")
	agentRoot := filepath.Join(homeDir, ".pi", "agent")
	selectedPath := filepath.Join(workDir, ".pi", "mcp.json")
	writeEffectiveConfig(t, selectedPath, `{"mcpServers":{"context7":{"command":"node"}}}`)

	observation := mustObservePiAdapter(t, PiAdapterInput{
		Contract: piEffectiveContract(t, target.ScopeProject),
		HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
	})
	if observation.State() != mcpeffective.StateExact {
		t.Fatalf("state = %q, want exact", observation.State())
	}
	sources := observation.Sources()
	if len(sources) != 6 {
		t.Fatalf("sources = %d, want six normal layers", len(sources))
	}
	wantIDs := []string{
		"shared-global",
		"agents-global",
		"agents-nested-global",
		"pi-global",
		"shared-project",
		"pi-project",
	}
	for index, wantID := range wantIDs {
		if sources[index].ID() != wantID || sources[index].Kind() != mcpeffective.SourceNormal {
			t.Fatalf("source[%d] = %q/%q, want %q/normal", index, sources[index].ID(), sources[index].Kind(), wantID)
		}
	}
	if sources[5].Precedence() != mcpeffective.PrecedenceSelected ||
		!sources[5].DefinesSelectedName() {
		t.Fatalf("selected source = %#v, want exact selected definition", sources[5])
	}
}

func TestObservePiAdapterBlocksSameNameInEveryOtherNormalLayer(t *testing.T) {
	tests := []struct {
		name string
		path func(homeDir string, workDir string, agentRoot string) string
	}{
		{name: "shared global", path: func(homeDir string, _ string, _ string) string {
			return filepath.Join(homeDir, ".config", "mcp", "mcp.json")
		}},
		{name: "agents global", path: func(homeDir string, _ string, _ string) string {
			return filepath.Join(homeDir, ".agents", "mcp.json")
		}},
		{name: "agents nested global", path: func(homeDir string, _ string, _ string) string {
			return filepath.Join(homeDir, ".agents", "mcp", "mcp.json")
		}},
		{name: "pi global", path: func(_ string, _ string, agentRoot string) string {
			return filepath.Join(agentRoot, "mcp.json")
		}},
		{name: "shared project", path: func(_ string, workDir string, _ string) string {
			return filepath.Join(workDir, ".mcp.json")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			homeDir := filepath.Join(root, "home")
			workDir := filepath.Join(root, "project")
			agentRoot := filepath.Join(homeDir, ".pi", "agent")
			selectedPath := filepath.Join(workDir, ".pi", "mcp.json")
			writeEffectiveConfig(t, selectedPath, `{"mcpServers":{"context7":{"command":"node"}}}`)
			writeEffectiveConfig(
				t,
				test.path(homeDir, workDir, agentRoot),
				`{"mcpServers":{"context7":{"url":"https://example.invalid"}}}`,
			)

			observation := mustObservePiAdapter(t, PiAdapterInput{
				Contract: piEffectiveContract(t, target.ScopeProject),
				HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
			})
			if observation.State() != mcpeffective.StateConflicting || !observation.LowerFallbackPresent() {
				t.Fatalf(
					"state/fallback = %q/%t, want conflicting/true",
					observation.State(),
					observation.LowerFallbackPresent(),
				)
			}
			if len(observation.BlockingSources()) != 1 {
				t.Fatalf("blocking sources = %#v, want one", observation.BlockingSources())
			}
		})
	}
}

func TestObservePiAdapterClassifiesHigherProjectConflictForGlobalPlacement(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	workDir := filepath.Join(root, "project")
	agentRoot := filepath.Join(homeDir, ".pi", "agent")
	selectedPath := filepath.Join(agentRoot, "mcp.json")
	writeEffectiveConfig(t, selectedPath, `{"mcpServers":{"context7":{"command":"node"}}}`)
	writeEffectiveConfig(
		t,
		filepath.Join(workDir, ".mcp.json"),
		`{"mcpServers":{"context7":{"command":"other"}}}`,
	)

	observation := mustObservePiAdapter(t, PiAdapterInput{
		Contract: piEffectiveContract(t, target.ScopeGlobal),
		HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
	})
	if observation.State() != mcpeffective.StateConflicting ||
		!observation.HigherConflictPresent() ||
		observation.LowerFallbackPresent() {
		t.Fatalf(
			"state/higher/lower = %q/%t/%t, want conflicting/true/false",
			observation.State(),
			observation.HigherConflictPresent(),
			observation.LowerFallbackPresent(),
		)
	}
}

func TestObservePiAdapterObservesPerLayerImportsAndHostDiscovery(t *testing.T) {
	tests := []struct {
		name            string
		selectedContent string
		importPath      func(homeDir string) string
	}{
		{
			name: "per-layer import",
			selectedContent: `{
				"imports": ["claude-code"],
				"mcpServers": {"context7": {"command": "node"}}
			}`,
			importPath: func(homeDir string) string {
				return filepath.Join(homeDir, ".claude", "mcp.json")
			},
		},
		{
			name: "host discovery",
			selectedContent: `{
				"settings": {"hostConfigDiscovery": "on"},
				"mcpServers": {"context7": {"command": "node"}}
			}`,
			importPath: func(homeDir string) string {
				return filepath.Join(homeDir, ".cursor", "mcp.json")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			homeDir := filepath.Join(root, "home")
			workDir := filepath.Join(root, "project")
			agentRoot := filepath.Join(homeDir, ".pi", "agent")
			selectedPath := filepath.Join(workDir, ".pi", "mcp.json")
			writeEffectiveConfig(t, selectedPath, test.selectedContent)
			writeEffectiveConfig(
				t,
				test.importPath(homeDir),
				`{"mcpServers":{"context7":{"command":"other"}}}`,
			)

			observation := mustObservePiAdapter(t, PiAdapterInput{
				Contract: piEffectiveContract(t, target.ScopeProject),
				HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
			})
			if observation.State() != mcpeffective.StateConflicting || !observation.LowerFallbackPresent() {
				t.Fatalf(
					"state/fallback = %q/%t, want conflicting/true",
					observation.State(),
					observation.LowerFallbackPresent(),
				)
			}
			blocking := observation.BlockingSources()
			if len(blocking) != 1 ||
				(blocking[0].Kind() != mcpeffective.SourceImport &&
					blocking[0].Kind() != mcpeffective.SourceHostDiscovery) {
				t.Fatalf("blocking sources = %#v, want one import/discovery source", blocking)
			}
		})
	}
}

func TestObservePiAdapterTreatsMissingImportAsExactAbsence(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	workDir := filepath.Join(root, "project")
	agentRoot := filepath.Join(homeDir, ".pi", "agent")
	selectedPath := filepath.Join(workDir, ".pi", "mcp.json")
	writeEffectiveConfig(t, selectedPath, `{
		"imports": ["claude-code"],
		"mcpServers": {"context7": {"command": "node"}}
	}`)

	observation := mustObservePiAdapter(t, PiAdapterInput{
		Contract: piEffectiveContract(t, target.ScopeProject),
		HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
	})
	if observation.State() != mcpeffective.StateExact || len(observation.BlockingSources()) != 0 {
		t.Fatalf("observation = %#v, want exact without blockers", observation)
	}
}

func TestObservePiAdapterTurnsOpaqueActiveSourcesIntoBlockers(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, path string)
		want    string
	}{
		{
			name: "malformed",
			prepare: func(t *testing.T, path string) {
				writeEffectiveConfig(t, path, `{"mcpServers":`)
			},
			want: "unexpected",
		},
		{
			name: "unknown import",
			prepare: func(t *testing.T, path string) {
				writeEffectiveConfig(t, path, `{"imports":["future-host"],"mcpServers":{}}`)
			},
			want: "unsupported",
		},
		{
			name: "symlink",
			prepare: func(t *testing.T, path string) {
				targetPath := filepath.Join(filepath.Dir(path), "real.json")
				writeEffectiveConfig(t, targetPath, `{"mcpServers":{}}`)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(targetPath, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			},
			want: "symlink",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			homeDir := filepath.Join(root, "home")
			workDir := filepath.Join(root, "project")
			agentRoot := filepath.Join(homeDir, ".pi", "agent")
			selectedPath := filepath.Join(workDir, ".pi", "mcp.json")
			test.prepare(t, filepath.Join(homeDir, ".agents", "mcp.json"))
			writeEffectiveConfig(t, selectedPath, `{"mcpServers":{"context7":{"command":"node"}}}`)

			observation := mustObservePiAdapter(t, PiAdapterInput{
				Contract: piEffectiveContract(t, target.ScopeProject),
				HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
			})
			if observation.State() != mcpeffective.StateUnobservable {
				t.Fatalf("state = %q, want unobservable", observation.State())
			}
			blocking := observation.BlockingSources()
			if len(blocking) != 1 || !strings.Contains(blocking[0].Detail(), test.want) {
				t.Fatalf("blocking sources = %#v, want detail containing %q", blocking, test.want)
			}
		})
	}
}

func TestObservePiAdapterCoalescesSharedAndCustomGlobalRoot(t *testing.T) {
	root := t.TempDir()
	homeDir := filepath.Join(root, "home")
	workDir := filepath.Join(root, "project")
	agentRoot := filepath.Join(homeDir, ".config", "mcp")
	selectedPath := filepath.Join(agentRoot, "mcp.json")
	writeEffectiveConfig(t, selectedPath, `{"mcpServers":{"context7":{"command":"node"}}}`)

	observation := mustObservePiAdapter(t, PiAdapterInput{
		Contract: piEffectiveContract(t, target.ScopeGlobal),
		HomeDir:  homeDir, WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selectedPath,
	})
	if observation.State() != mcpeffective.StateExact {
		t.Fatalf("state = %q, want exact", observation.State())
	}
	if len(observation.Sources()) != 5 {
		t.Fatalf("sources = %d, want coalesced five layers", len(observation.Sources()))
	}
	selectedCount := 0
	for _, source := range observation.Sources() {
		if source.Precedence() == mcpeffective.PrecedenceSelected {
			selectedCount++
		}
	}
	if selectedCount != 1 {
		t.Fatalf("selected source count = %d, want one", selectedCount)
	}
}

func piEffectiveContract(t *testing.T, scope target.Scope) lock.LockedSubjectContract {
	t.Helper()
	provider := desiredtest.Extension(t, desiredextension.Spec{
		Name:    "pi-mcp-adapter-" + string(scope),
		Carrier: desiredextension.CarrierPiPackage,
		Target:  target.TargetPi,
		Scope:   scope,
		Source: desiredtest.ExtensionSource(
			t,
			desiredextension.SourceKindHostSource,
			"npm:pi-mcp-adapter@^2.13.0",
		),
	})
	transport := desiredtest.MCPStdio(
		t,
		desiredtest.MCPCommand(t, "node"),
		[]string{"server.js"},
		nil,
	)
	binding := desiredtest.MCPBinding(
		t,
		target.TargetPi,
		scope,
		transport,
		desiredmcp.OnAbsentRemoveBinding,
	)
	server := desiredtest.MCPServer(t, desiredmcp.Spec{
		Name:     "context7",
		Bindings: []desiredmcp.Binding{binding},
	})
	contracts, err := lockrefine.MCPSubjects(
		[]desiredmcp.Server{server},
		[]desiredextension.Extension{provider},
		mcpcodec.CanonicalMCPBindingContribution,
	)
	if err != nil {
		t.Fatalf("MCPSubjects returned error: %v", err)
	}
	if len(contracts) != 1 {
		t.Fatalf("MCP contracts = %d, want one", len(contracts))
	}
	return contracts[0]
}

func mustObservePiAdapter(t *testing.T, input PiAdapterInput) mcpeffective.Observation {
	t.Helper()
	observation, err := ObservePiAdapter(input)
	if err != nil {
		t.Fatalf("ObservePiAdapter returned error: %v", err)
	}
	return observation
}

func writeEffectiveConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
