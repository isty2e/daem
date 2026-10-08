package lockfile

import (
	"fmt"
	"slices"
	"sort"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestPiMCPContextAdmissionAcrossProducersAndStoredCollections(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
		peerScope := target.ScopeProject
		if scope == target.ScopeProject {
			peerScope = target.ScopeGlobal
		}
		for _, test := range []struct {
			name       string
			backends   []desiredmcp.Backend
			packageRef string
			accepted   bool
		}{
			{"native", []desiredmcp.Backend{desiredmcp.BackendNative}, "", true},
			{"scoped native peers", []desiredmcp.Backend{desiredmcp.BackendNative, desiredmcp.BackendNative}, "", true},
			{"adapter", []desiredmcp.Backend{desiredmcp.BackendAdapter}, "npm:pi-mcp-adapter@^2.13.0", true},
			{"mixed scoped backends", []desiredmcp.Backend{desiredmcp.BackendNative, desiredmcp.BackendAdapter}, "npm:pi-mcp-adapter@^2.13.0", false},
			{"native and adapter carrier only", []desiredmcp.Backend{desiredmcp.BackendNative}, "npm:pi-mcp-adapter@^2.13.0", false},
			{"native and unrelated carrier", []desiredmcp.Backend{desiredmcp.BackendNative}, "npm:unrelated@1.0.0", true},
			{"standalone older carrier", nil, "npm:pi-mcp-adapter@1.0.0", true},
		} {
			for _, reversed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/reversed=%t", test.name, scope, reversed), func(t *testing.T) {
					extensions := []desiredextension.Extension{}
					if test.packageRef != "" {
						extensions = append(extensions, desiredtest.Extension(t, desiredextension.Spec{
							Name: "provider", Carrier: desiredextension.CarrierPiPackage,
							Target: target.TargetPi, Scope: target.ScopeGlobal,
							Source: desiredtest.ExtensionSource(t, desiredextension.SourceKindHostSource, test.packageRef),
						}))
					}
					contracts, err := lockrefine.Extensions(extensions)
					if err != nil {
						t.Fatal(err)
					}
					order, err := lockrefine.ExtensionOrderConstraints(extensions, nil)
					if err != nil {
						t.Fatal(err)
					}

					servers := make([]desiredmcp.Server, 0, len(test.backends))
					for index, backend := range test.backends {
						bindingScope := scope
						if index != 0 {
							bindingScope = peerScope
						}
						transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "node"), nil, nil)
						binding, err := desiredmcp.NewBindingWithBackend(target.TargetPi, bindingScope, transport, desiredmcp.OnAbsentRemoveBinding, backend)
						if err != nil {
							t.Fatal(err)
						}
						server := desiredtest.MCPServer(t, desiredmcp.Spec{Name: fmt.Sprintf("server-%d", index), Bindings: []desiredmcp.Binding{binding}})
						servers = append(servers, server)

						providers := extensions
						if backend == desiredmcp.BackendNative {
							providers = nil
						}
						individual, err := lockrefine.MCPSubjects([]desiredmcp.Server{server}, providers, mcpcodec.CanonicalMCPBindingContribution)
						if err != nil || len(individual) != 1 {
							t.Fatalf("individual contract fixture: %d, %v", len(individual), err)
						}
						contracts = append(contracts, individual...)
					}
					if reversed {
						slices.Reverse(servers)
						slices.Reverse(contracts)
					}

					refined, err := lockrefine.MCPSubjects(servers, extensions, mcpcodec.CanonicalMCPBindingContribution)
					if (err == nil) != test.accepted || (!test.accepted && len(refined) != 0) {
						t.Errorf("whole producer: %d subjects, %v; accepted=%t", len(refined), err, test.accepted)
					}
					section, err := lock.NewLockedSection(contracts, order)
					if (err == nil) != test.accepted || (!test.accepted && section.Len() != 0) {
						t.Errorf("complete collection: %d subjects, %v; accepted=%t", section.Len(), err, test.accepted)
					}

					sort.Slice(contracts, func(left, right int) bool { return contracts[left].CompareIdentity(contracts[right]) < 0 })
					dtos, err := subjectsToDTO(contracts)
					if err != nil {
						t.Fatal(err)
					}
					orderDTOs, err := orderConstraintsToDTO(order)
					if err != nil {
						t.Fatal(err)
					}
					wire, err := encodeNativeLockDTO(fileDTO{Version: currentLockfileVersion, Locked: lockedSectionDTO{Subjects: dtos, OrderConstraints: orderDTOs}})
					if err != nil {
						t.Fatal(err)
					}
					loaded, err := Load(t.Context(), writeLockfileText(t, string(wire)))
					if (err == nil) != test.accepted || (!test.accepted && loaded.Locked.Len() != 0) {
						t.Errorf("stored DTO load: %d subjects, %v; accepted=%t", loaded.Locked.Len(), err, test.accepted)
					}
					if err == nil {
						assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), section.Subjects())
					}
				})
			}
		}
	}
}
