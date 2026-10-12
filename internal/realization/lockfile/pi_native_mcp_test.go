package lockfile

import (
	"bytes"
	"slices"
	"sort"
	"testing"

	"github.com/BurntSushi/toml"
	desiredmcp "github.com/isty2e/daem/internal/desired/mcp"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	"github.com/isty2e/daem/internal/realization/lock"
	lockrefine "github.com/isty2e/daem/internal/realization/lock/refine"
	"github.com/isty2e/daem/internal/target"
)

func TestPiNativeMCPNamespaceAdmissionAcrossScopes(t *testing.T) {
	for _, test := range []struct {
		name        string
		firstName   string
		secondName  string
		firstScope  target.Scope
		secondScope target.Scope
		wantError   bool
	}{
		{"global-project collision", "foo-bar", "foo_bar", target.ScopeGlobal, target.ScopeProject, true},
		{"project collision", "foo-bar", "foo_bar", target.ScopeProject, target.ScopeProject, true},
		{"global collision", "foo-bar", "foo_bar", target.ScopeGlobal, target.ScopeGlobal, true},
		{"repeated hyphen collision", "foo--bar", "foo__bar", target.ScopeGlobal, target.ScopeProject, true},
		{"exact-name override", "foo-bar", "foo-bar", target.ScopeGlobal, target.ScopeProject, false},
		{"distinct namespaces", "foo-bar", "foo_baz", target.ScopeGlobal, target.ScopeProject, false},
	} {
		for _, reversed := range []bool{false, true} {
			name := test.name + "/forward"
			if reversed {
				name = test.name + "/reversed"
			}
			t.Run(name, func(t *testing.T) {
				first := nativePiServer(t, test.firstName, test.firstScope)
				second := nativePiServer(t, test.secondName, test.secondScope)
				servers := []desiredmcp.Server{first, second}
				if reversed {
					slices.Reverse(servers)
				}
				if test.firstName == test.secondName {
					scopes := []target.Scope{test.firstScope, test.secondScope}
					if reversed {
						slices.Reverse(scopes)
					}
					servers = []desiredmcp.Server{nativePiServer(t, test.firstName, scopes...)}
				}

				refined, err := lockrefine.MCPSubjects(servers, nil, mcpcodec.CanonicalMCPBindingContribution)
				if (err != nil) != test.wantError {
					t.Fatalf("declared collection admission: subjects=%d, err=%v, want error=%t", len(refined), err, test.wantError)
				}
				if test.wantError && len(refined) != 0 {
					t.Fatal("rejected collection returned partial locked subjects")
				}

				contracts := make([]lock.LockedSubjectContract, 0, 2)
				for _, server := range servers {
					for _, binding := range server.Bindings() {
						contract, err := lockrefine.MCPBindingSubject(server, binding, mcpcodec.CanonicalMCPBindingContribution)
						if err != nil {
							t.Fatal(err)
						}
						contracts = append(contracts, contract)
					}
				}
				section, err := lock.NewLockedSection(contracts, nil)
				if (err != nil) != test.wantError {
					t.Fatalf("independent subject collection admission: len=%d, err=%v, want error=%t", section.Len(), err, test.wantError)
				}

				sort.Slice(contracts, func(left, right int) bool {
					return contracts[left].CompareIdentity(contracts[right]) < 0
				})
				dtos, err := subjectsToDTO(contracts)
				if err != nil {
					t.Fatal(err)
				}
				wire, err := encodeNativeLockDTO(fileDTO{Version: currentLockfileVersion, Locked: lockedSectionDTO{Subjects: dtos}})
				if err != nil {
					t.Fatal(err)
				}
				loaded, err := Load(t.Context(), writeLockfileText(t, string(wire)))
				if (err != nil) != test.wantError {
					t.Fatalf("serialized collection admission: len=%d, err=%v, want error=%t", loaded.Locked.Len(), err, test.wantError)
				}
				if !test.wantError {
					assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), section.Subjects())
				}
			})
		}
	}
}

func TestPiNativeMCPRemovalContractRoundTrip(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			server := nativePiServer(t, "context7", scope)
			contracts, err := lockrefine.MCPSubjects([]desiredmcp.Server{server}, nil, mcpcodec.CanonicalMCPBindingContribution)
			if err != nil {
				t.Fatal(err)
			}
			file := lockfileWithSubjects(t, contracts...)
			wire, err := Marshal(file)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(t.Context(), writeLockfileText(t, string(wire)))
			if err != nil {
				t.Fatal(err)
			}
			assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())

			for _, subject := range []lock.LockedSubjectContract{contracts[0], loaded.Locked.Subjects()[0]} {
				remove, present := subject.OperationContract(lock.OperationRemoveProjection)
				if !present {
					t.Fatal("native contract has no remove operation")
				}
				want := []string{"managed_binding_baseline", "native_config_strict_json", "unsupported_managed_fields_absent"}
				if !slices.Equal(remove.Preconditions(), want) {
					t.Fatalf("native removal prerequisites = %v, want %v", remove.Preconditions(), want)
				}
				if _, present := subject.MCPProviderContribution(); present {
					t.Fatal("native removal acquired an Adapter provider")
				}
			}
		})
	}
}

func TestPiNativeMCPRejectsInheritedAdapterRemovalMetadata(t *testing.T) {
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			server := nativePiServer(t, "context7", scope)
			contract, err := lockrefine.MCPBindingSubject(server, server.Bindings()[0], mcpcodec.CanonicalMCPBindingContribution)
			if err != nil {
				t.Fatal(err)
			}
			dto, err := subjectToDTO(contract)
			if err != nil {
				t.Fatal(err)
			}
			for index := range dto.Operations {
				if dto.Operations[index].Operation == string(lock.OperationRemoveProjection) {
					dto.Operations[index].Preconditions = []string{
						"adapter_contract_current", "effective_config_collision_free", "managed_binding_baseline",
						string(scope) + "_adapter_config_strict_json", "provider_contribution_available",
						"provider_jsonc_absent", "provider_version_compatible", "unsupported_managed_fields_absent",
					}
					slices.Sort(dto.Operations[index].Preconditions)
				}
			}
			wire, err := encodeNativeLockDTO(fileDTO{Version: currentLockfileVersion, Locked: lockedSectionDTO{Subjects: []lockedSubjectDTO{dto}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Load(t.Context(), writeLockfileText(t, string(wire))); err == nil {
				t.Fatal("Native lock carrying Adapter removal requirements was admitted")
			}
		})
	}
}

func nativePiServer(t *testing.T, name string, scopes ...target.Scope) desiredmcp.Server {
	t.Helper()
	transport := desiredtest.MCPStdio(t, desiredtest.MCPCommand(t, "node"), []string{"server.js"}, nil)
	bindings := make([]desiredmcp.Binding, 0, len(scopes))
	for _, scope := range scopes {
		binding, err := desiredmcp.NewBindingWithBackend(target.TargetPi, scope, transport, desiredmcp.OnAbsentRemoveBinding, desiredmcp.BackendNative)
		if err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	return desiredtest.MCPServer(t, desiredmcp.Spec{Name: name, Bindings: bindings})
}

func encodeNativeLockDTO(dto fileDTO) ([]byte, error) {
	var output bytes.Buffer
	if err := toml.NewEncoder(&output).Encode(dto); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
