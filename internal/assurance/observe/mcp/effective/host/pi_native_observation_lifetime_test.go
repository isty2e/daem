package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpobserve "github.com/isty2e/daem/internal/assurance/observe/mcp"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/hostpath"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativePhysicalSourceAliasesRefuseIndependentEvidence(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
		for _, retiring := range []bool{false, true} {
			for _, present := range []bool{false, true} {
				for _, ancestor := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/retiring=%t/present=%t/ancestor=%t", scope, retiring, present, ancestor), func(t *testing.T) {
						root := t.TempDir()
						workDir := filepath.Join(root, "project")
						projectRoot := filepath.Join(workDir, ".pi")
						if err := os.MkdirAll(projectRoot, 0o700); err != nil {
							t.Fatal(err)
						}
						link := filepath.Join(root, "alias")
						linkTarget, agentRoot := projectRoot, link
						if ancestor {
							linkTarget, agentRoot = workDir, filepath.Join(link, ".pi")
						}
						if err := os.Symlink(linkTarget, link); err != nil {
							t.Skipf("directory symlinks unavailable: %v", err)
						}
						if present {
							writeEffectiveConfig(t, filepath.Join(projectRoot, "mcp.json"), `{"mcpServers":{"context7":{"command":"node"}}}`)
						}
						selected := filepath.Join(agentRoot, "mcp.json")
						if scope == target.ScopeProject {
							selected = filepath.Join(projectRoot, "mcp.json")
						}

						observation, err := ObservePiNative(PiNativeInput{
							Projection: piNativeProjection(t, scope), Codecs: aggregatecodec.Catalog(),
							WorkDir: workDir, AgentRoot: agentRoot, SelectedPath: selected, Retiring: retiring,
						})
						if err == nil || len(observation.Sources()) != 0 {
							t.Fatalf("one physical source supplied independent evidence: %v, %v", observation.Sources(), err)
						}
					})
				}
			}
		}
	}
}

func TestPiNativeSourceContextRetainsDistinctDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	workDir, agentRoot := filepath.Join(root, "project"), filepath.Join(root, "agent")
	globalPath, projectPath := filepath.Join(agentRoot, "mcp.json"), filepath.Join(workDir, ".pi", "mcp.json")
	for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
		selected := globalPath
		if scope == target.ScopeProject {
			selected = projectPath
		}
		if _, err := newPiNativeSourceContext(scope, workDir, agentRoot, selected); err != nil {
			t.Fatalf("distinct missing config entries refused: %v", err)
		}
	}

	writeEffectiveConfig(t, globalPath, `{"mcpServers":{}}`)
	if err := os.MkdirAll(filepath.Dir(projectPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(globalPath, projectPath); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	if _, err := newPiNativeSourceContext(target.ScopeGlobal, workDir, agentRoot, globalPath); err != nil {
		t.Fatalf("different directory entries were conflated by shared file content: %v", err)
	}
}

func TestPiNativeQualificationLifetimeIsOperationAndScopeLocal(t *testing.T) {
	workDir, agentRoot := t.TempDir(), t.TempDir()
	queryLog := installNativeQueryCanary(t)
	t.Setenv("PI_CODING_AGENT_DIR", agentRoot)
	settingsPath := filepath.Join(agentRoot, "settings.json")
	settings := `{}`
	writeEffectiveConfig(t, settingsPath, settings)
	resolver := hostpath.NewResolver(workDir).WithDestinationOverride(pihostpath.DestinationOverride(workDir))
	input := Input{Context: t.Context(), Codecs: aggregatecodec.Catalog(), WorkDir: workDir, ResolveDestination: resolver.Resolve}

	for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
		t.Run(string(scope), func(t *testing.T) {
			writeEffectiveConfig(t, settingsPath, settings)
			input.Contracts = nativeCatalogContracts(t, scope, "one")
			observed, err := Observe(input)
			if err != nil || len(observed.Current) != 1 {
				t.Fatalf("single binding observation = %#v, %v", observed, err)
			}
			if observed.HostPrerequisites[input.Contracts[0].SubjectID()].State() != mcpobserve.HostQualified {
				t.Fatal("single binding lacks qualification evidence")
			}

			input.Contracts = nativeCatalogContracts(t, scope, "one", "two", "three")
			observed, err = Observe(input)
			if err != nil || len(observed.Current) != len(input.Contracts) {
				t.Fatalf("multiple binding observation = %#v, %v", observed, err)
			}
			for _, contract := range input.Contracts {
				if observed.HostPrerequisites[contract.SubjectID()].State() != mcpobserve.HostQualified {
					t.Fatal("qualification was not attributed to every binding")
				}
			}

			writeEffectiveConfig(t, settingsPath, `{"packages":[{"source":"npm:pi-mcp-adapter@2.15.0","extensions":[]}]}`)
			observed, err = Observe(input)
			if err != nil || len(observed.Current) != len(input.Contracts) {
				t.Fatalf("host refusal suppressed current observations: %#v, %v", observed, err)
			}
			for _, contract := range input.Contracts {
				host := observed.HostPrerequisites[contract.SubjectID()]
				if host.State() != mcpobserve.HostUnqualified || host.Reason() != mcpobserve.ReasonHostAdapterConfigured {
					t.Fatalf("later operation reused stale qualification: %#v", host)
				}
			}
			assertNativeQueryNotCalled(t, queryLog)
		})
	}

	t.Run("scope-specific builtin refusal", func(t *testing.T) {
		writeEffectiveConfig(t, settingsPath, `{"extensions":["-builtin:mcp"]}`)
		writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{"extensions":["+builtin:mcp"]}`)
		input.Contracts = append(nativeCatalogContracts(t, target.ScopeProject, "one"), nativeCatalogContracts(t, target.ScopeGlobal, "two")...)
		observed, err := Observe(input)
		if err != nil || len(observed.Current) != 2 {
			t.Fatalf("scope-local qualification suppressed observations: %#v, %v", observed, err)
		}
		for index, state := range []mcpobserve.HostPrerequisiteState{mcpobserve.HostQualified, mcpobserve.HostUnqualified} {
			if host := observed.HostPrerequisites[input.Contracts[index].SubjectID()]; host.State() != state {
				t.Fatalf("scope %d reused another scope's qualification: %#v", index, host)
			}
		}
	})

	t.Run("cancellation between bindings", func(t *testing.T) {
		writeEffectiveConfig(t, settingsPath, settings)
		writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "settings.json"), `{}`)
		input.Contracts = nativeCatalogContracts(t, target.ScopeProject, "one", "two")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		input.Context = ctx
		canceledAfterQualification := false
		resolved := 0
		input.ResolveDestination = func(destination output.Destination) (string, error) {
			resolved++
			if resolved == 2 {
				cancel()
				canceledAfterQualification = true
			}
			return resolver.Resolve(destination)
		}
		observed, err := Observe(input)
		if !canceledAfterQualification || !errors.Is(err, context.Canceled) || len(observed.Current) != 0 || len(observed.Retiring) != 0 || len(observed.HostPrerequisites) != 0 {
			t.Fatalf("qualification reuse bypassed cancellation: %#v, %v", observed, err)
		}
	})

	t.Run("retirement does not qualify", func(t *testing.T) {
		input.Context = t.Context()
		input.ResolveDestination = resolver.Resolve
		input.Contracts = nil
		projection, present, err := piNativeCatalogContract(t, target.ScopeGlobal).ManagedAggregateContribution()
		if err != nil || !present {
			t.Fatalf("retiring projection: %t, %v", present, err)
		}
		input.Retiring = []aggregate.SubjectContribution{projection}
		t.Setenv("PATH", t.TempDir())
		observed, err := Observe(input)
		if err != nil || len(observed.Retiring) != 1 || len(observed.HostPrerequisites) != 0 {
			t.Fatalf("retirement acquired current qualification: %#v, %v", observed, err)
		}
		assertNativeQueryNotCalled(t, queryLog)
	})
}

func nativeCatalogContracts(t *testing.T, scope target.Scope, names ...string) []lock.LockedSubjectContract {
	t.Helper()
	var text strings.Builder
	text.WriteString("version = 1\ntargets = [\"pi\"]\n")
	for _, name := range names {
		fmt.Fprintf(&text, "\n[[mcp_server]]\nname = %q\ntargets = [\"pi\"]\nscope = %q\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n", name, scope)
	}
	manifest, err := declarationmanifest.Decode([]byte(text.String()))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(manifest.MCPServers(), nil, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != len(names) {
		t.Fatalf("native contract refinement: len=%d, err=%v", len(contracts), err)
	}
	return contracts
}
