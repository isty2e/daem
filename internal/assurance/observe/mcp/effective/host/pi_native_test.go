package host

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	mcpeffective "github.com/isty2e/daem/internal/assurance/observe/mcp/effective"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeObservationReplacementOverridesAndConservativeRetirement(t *testing.T) {
	for _, test := range []struct {
		name     string
		scope    target.Scope
		global   string
		project  string
		retiring bool
		state    mcpeffective.State
		fallback bool
	}{
		{"project replaces lower", target.ScopeProject, `{"mcpServers":{"context7":{"command":"echo"}}}`, `{"mcpServers":{"context7":{"command":"node"}}}`, false, mcpeffective.StateExact, true},
		{"global same full replacement", target.ScopeGlobal, `{"mcpServers":{"context7":{"command":"node"}}}`, `{"mcpServers":{"context7":{"command":"node"}}}`, false, mcpeffective.StateExact, false},
		{"global different replacement", target.ScopeGlobal, `{"mcpServers":{"context7":{"command":"node"}}}`, `{"mcpServers":{"context7":{"command":"echo"}}}`, false, mcpeffective.StateConflicting, false},
		{"global enabled override", target.ScopeGlobal, `{"mcpServers":{"context7":{"command":"node"}}}`, `{"mcpServers":{"context7":{"enabled":true}}}`, false, mcpeffective.StateExact, false},
		{"global disabled override", target.ScopeGlobal, `{"mcpServers":{"context7":{"command":"node"}}}`, `{"mcpServers":{"context7":{"enabled":false}}}`, false, mcpeffective.StateConflicting, false},
		{"global exposure override", target.ScopeGlobal, `{"mcpServers":{"context7":{"command":"node"}}}`, `{"mcpServers":{"context7":{"exposure":"direct"}}}`, false, mcpeffective.StateConflicting, false},
		{"global tool override", target.ScopeGlobal, `{"mcpServers":{"context7":{"command":"node"}}}`, `{"mcpServers":{"context7":{"toolExposure":{"x":"direct"}}}}`, false, mcpeffective.StateConflicting, false},
		{"retirement exposes lower", target.ScopeProject, `{"mcpServers":{"context7":{"command":"echo"}}}`, `{"mcpServers":{"context7":{"command":"node"}}}`, true, mcpeffective.StateConflicting, true},
		{"native aliases inert", target.ScopeProject, `{}`, `{"servers":{"context7":{"command":"echo"}}}`, false, mcpeffective.StateExact, false},
		{"jsonc opaque", target.ScopeProject, `{}`, "{//comment\n\"mcpServers\":{}}", false, mcpeffective.StateUnobservable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workDir := filepath.Join(root, "project")
			agentRoot := filepath.Join(root, "agent")
			globalPath := filepath.Join(agentRoot, "mcp.json")
			projectPath := filepath.Join(workDir, ".pi", "mcp.json")
			writeEffectiveConfig(t, globalPath, test.global)
			writeEffectiveConfig(t, projectPath, test.project)
			selected := projectPath
			if test.scope == target.ScopeGlobal {
				selected = globalPath
			}
			observation, err := ObservePiNative(PiNativeInput{
				Projection: piNativeProjection(t, test.scope), Codecs: aggregatecodec.Catalog(),
				WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selected, Retiring: test.retiring,
			})
			if err != nil || observation.State() != test.state || observation.LowerFallbackPresent() != test.fallback {
				t.Fatalf("observation = %q/%t, %v; want %q/%t", observation.State(), observation.LowerFallbackPresent(), err, test.state, test.fallback)
			}
			if len(observation.Sources()) != 2 {
				t.Fatal("native observation used adapter source layers")
			}
		})
	}
}

func TestPiNativeSettingsQualification(t *testing.T) {
	contract, _ := profile.PiMCPContractForCodec(aggregate.MCPCodecPiNativeStdio)
	for _, test := range []struct {
		global, project string
		accepted        bool
	}{
		{`{}`, `{}`, true},
		{`{"extensions":["-builtin:mcp"]}`, `{}`, false},
		{`{}`, `{"extensions":["-builtin:mcp"]}`, false},
		{`{"extensions":["-builtin:mcp"]}`, `{"extensions":["+builtin:mcp"]}`, true},
		{`{"packages":["npm:pi-mcp-adapter@^2.13.0"]}`, `{}`, false},
		{`{}`, `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0"}]}`, false},
		{`{"packages":["npm:unrelated@1.0.0"]}`, `{}`, true},
		{`{"extensions":null}`, `{}`, false},
	} {
		root := t.TempDir()
		workDir, agentRoot := filepath.Join(root, "project"), filepath.Join(root, "agent")
		writeEffectiveConfig(t, filepath.Join(agentRoot, "settings.json"), test.global)
		writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), test.project)
		err := qualifyPiNativeSettings(contract, target.ScopeProject, workDir, agentRoot, profile.ObservePiMCPVersion("1.0.2"))
		if (err == nil) != test.accepted {
			t.Fatalf("qualification for %s/%s = %v", test.global, test.project, err)
		}
	}
}

func TestPiNativeVersionObservationIsolatesGlobalAndProjectSettings(t *testing.T) {
	root := t.TempDir()
	log := filepath.Join(root, "observed.txt")
	fake := filepath.Join(root, "pi")
	script := "#!/bin/sh\n[ \"$1\" = --version ] || exit 91\nprintf '%s\\n%s\\n' \"$PI_CODING_AGENT_DIR\" \"$PWD\" > \"$DAEM_VERSION_TEST_LOG\"\nprintf '1.0.2\\n'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(root, "personal-agent"))
	t.Setenv("DAEM_VERSION_TEST_LOG", log)
	version, err := ObservePiVersion(t.Context())
	if err != nil || !version.NativeCompatible() {
		t.Fatalf("version = %#v, %v", version, err)
	}
	content, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	paths := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(paths) != 2 || paths[0] == os.Getenv("PI_CODING_AGENT_DIR") {
		t.Fatalf("non-isolated bootstrap paths: %q", content)
	}
	globalParent, err := filepath.EvalSymlinks(filepath.Dir(paths[0]))
	if err != nil {
		t.Fatal(err)
	}
	projectParent, err := filepath.EvalSymlinks(filepath.Dir(paths[1]))
	if err != nil || filepath.Join(globalParent, filepath.Base(paths[0])) != filepath.Join(projectParent, filepath.Base(paths[1])) {
		t.Fatalf("bootstrap roots do not share the disposable directory: %q, %v", content, err)
	}
	if _, err := os.Stat(paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("version sandbox was not removed: %v", err)
	}
}

func TestPiNativeVersionCancellationNeverBecomesFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ObservePiVersion(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled observation = %v", err)
	}
	root := t.TempDir()
	fake := filepath.Join(root, "pi")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec /bin/sleep 30\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", root)
	ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	if _, err := ObservePiVersion(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("interrupted observation = %v", err)
	}
}

func piNativeProjection(t *testing.T, scope target.Scope) aggregate.SubjectContribution {
	t.Helper()
	content := "version = 1\ntargets = [\"pi\"]\n\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"" + string(scope) + "\"\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n"
	manifest, err := declarationmanifest.Decode([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(manifest.MCPServers(), nil, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("native lock refinement = %#v, %v", contracts, err)
	}
	projection, present, err := contracts[0].ManagedAggregateContribution()
	if err != nil || !present {
		t.Fatalf("native projection = %t, %v", present, err)
	}
	return projection
}
