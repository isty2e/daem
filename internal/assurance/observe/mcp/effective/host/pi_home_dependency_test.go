package host

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/output/hostpath"
	pihostpath "github.com/isty2e/daem/internal/output/hostpath/pi"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeCatalogObservesExplicitRootsWithoutHome(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		for _, relative := range []bool{false, true} {
			for _, retiring := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/relative=%t/retiring=%t", scope, relative, retiring), func(t *testing.T) {
					workDir := t.TempDir()
					agentRoot := filepath.Join(workDir, "agent")
					configuredRoot := agentRoot
					if relative {
						configuredRoot = "agent"
					}
					withoutPiUserHome(t)
					t.Setenv("PI_CODING_AGENT_DIR", configuredRoot)
					t.Setenv("PATH", t.TempDir())
					if !retiring {
						bin := t.TempDir()
						if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 91\nprintf '1.0.2\\n'\n"), 0o700); err != nil {
							t.Fatal(err)
						}
						t.Setenv("PATH", bin)
					}
					writeEffectiveConfig(t, filepath.Join(agentRoot, "mcp.json"), `{"mcpServers":{"context7":{"command":"node"}}}`)
					writeEffectiveConfig(t, filepath.Join(workDir, ".pi", "mcp.json"), `{"mcpServers":{"context7":{"command":"node"}}}`)
					resolver := hostpath.NewResolver(workDir).WithDestinationOverride(pihostpath.DestinationOverride(workDir))
					contract := piNativeCatalogContract(t, scope)
					input := Input{Context: t.Context(), Codecs: aggregatecodec.Catalog(), WorkDir: workDir, ResolveDestination: resolver.Resolve}
					if retiring {
						projection, present, err := contract.ManagedAggregateContribution()
						if err != nil || !present {
							t.Fatalf("native projection: present=%t, err=%v", present, err)
						}
						input.Retiring = []aggregate.SubjectContribution{projection}
					} else {
						input.Contracts = []lock.LockedSubjectContract{contract}
					}

					observed, err := Observe(input)
					if err != nil {
						t.Fatalf("Native explicit-root observation required unrelated home: %v", err)
					}
					observations := observed.Current
					if retiring {
						observations = observed.Retiring
					}
					if len(observations) != 1 || len(observed.Current)+len(observed.Retiring) != 1 {
						t.Fatalf("selected Native observations = %#v", observed)
					}
					selected := filepath.Join(workDir, ".pi", "mcp.json")
					if scope == target.ScopeGlobal {
						selected = filepath.Join(agentRoot, "mcp.json")
					}
					if observations[0].SelectedPath() != selected {
						t.Fatalf("selected path = %q, want %q", observations[0].SelectedPath(), selected)
					}
				})
			}
		}
	}
}

func TestPiCatalogPreservesBackendHomeRequirementsAndRootRefusals(t *testing.T) {
	for _, configuredRoot := range []string{"", "~/agent", "invalid-root "} {
		t.Run("Native root refusal/"+configuredRoot, func(t *testing.T) {
			workDir := t.TempDir()
			withoutPiUserHome(t)
			t.Setenv("PI_CODING_AGENT_DIR", configuredRoot)
			resolver := hostpath.NewResolver(workDir).WithDestinationOverride(pihostpath.DestinationOverride(workDir))
			if _, err := Observe(Input{
				Context: t.Context(), Codecs: aggregatecodec.Catalog(), WorkDir: workDir, ResolveDestination: resolver.Resolve,
				Retiring: []aggregate.SubjectContribution{piNativeProjection(t, target.ScopeProject)},
			}); err == nil {
				t.Fatal("Native observation bypassed required root resolution")
			}
		})
	}
	for _, reversed := range []bool{false, true} {
		for _, missingHome := range []bool{false, true} {
			t.Run(fmt.Sprintf("mixed backends/reversed=%t/missingHome=%t", reversed, missingHome), func(t *testing.T) {
				workDir := t.TempDir()
				home := filepath.Join(workDir, "home")
				for _, variable := range []string{"HOME", "USERPROFILE", "home"} {
					t.Setenv(variable, home)
				}
				if missingHome {
					withoutPiUserHome(t)
				}
				t.Setenv("PI_CODING_AGENT_DIR", filepath.Join(workDir, "agent"))
				t.Setenv("PATH", t.TempDir())
				projections := []aggregate.SubjectContribution{piNativeProjection(t, target.ScopeProject), piEffectiveProjection(t, target.ScopeGlobal)}
				if reversed {
					slices.Reverse(projections)
				}
				resolver := hostpath.NewResolver(workDir).WithDestinationOverride(pihostpath.DestinationOverride(workDir))
				observed, err := Observe(Input{
					Context: t.Context(), Codecs: aggregatecodec.Catalog(), WorkDir: workDir, ResolveDestination: resolver.Resolve,
					Retiring: projections,
				})
				if (err != nil) != missingHome {
					t.Fatalf("backend-specific home admission: %v, want error=%t", err, missingHome)
				}
				if missingHome && (len(observed.Current) != 0 || len(observed.Retiring) != 0) {
					t.Fatal("failed Adapter prerequisite returned partial observations")
				}
				if !missingHome && len(observed.Retiring) != 2 {
					t.Fatalf("mixed retiring observation lost a backend: %#v", observed)
				}
			})
		}
	}
}

func withoutPiUserHome(t *testing.T) {
	t.Helper()
	for _, variable := range []string{"HOME", "USERPROFILE", "home"} {
		t.Setenv(variable, "")
	}
	if _, err := os.UserHomeDir(); err == nil {
		t.Skip("platform does not support an unavailable user-home fixture")
	}
}

func piNativeCatalogContract(t *testing.T, scope target.Scope) lock.LockedSubjectContract {
	t.Helper()
	manifest, err := declarationmanifest.Decode([]byte("version = 1\ntargets = [\"pi\"]\n[[mcp_server]]\nname = \"context7\"\ntargets = [\"pi\"]\nscope = \"" + string(scope) + "\"\nbackend = \"native\"\ntransport = \"stdio\"\ncommand = \"node\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := lockrefine.MCPSubjects(manifest.MCPServers(), nil, mcpcodec.CanonicalMCPBindingContribution)
	if err != nil || len(contracts) != 1 {
		t.Fatalf("native contract refinement: len=%d, err=%v", len(contracts), err)
	}
	return contracts[0]
}
