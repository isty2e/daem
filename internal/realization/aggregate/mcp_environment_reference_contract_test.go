package aggregate_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

func TestMCPEnvReferenceContractAdmitsOnlyValidCrossProducts(t *testing.T) {
	tests := []struct {
		name       string
		mapping    aggregate.MCPEnvReferenceMapping
		resolution aggregate.MCPEnvReferenceResolution
		wantError  string
	}{
		{
			name:       "unsupported and unavailable",
			mapping:    aggregate.MCPEnvMappingUnsupported,
			resolution: aggregate.MCPEnvResolutionUnavailable,
		},
		{
			name:       "same name at host runtime",
			mapping:    aggregate.MCPEnvMappingSameName,
			resolution: aggregate.MCPEnvResolutionHostRuntime,
		},
		{
			name:       "aliased at host runtime",
			mapping:    aggregate.MCPEnvMappingAliased,
			resolution: aggregate.MCPEnvResolutionHostRuntime,
		},
		{
			name:      "zero values",
			wantError: "unsupported MCP environment-reference mapping",
		},
		{
			name:       "unsupported at host runtime",
			mapping:    aggregate.MCPEnvMappingUnsupported,
			resolution: aggregate.MCPEnvResolutionHostRuntime,
			wantError:  "require unavailable resolution",
		},
		{
			name:       "same name without resolution",
			mapping:    aggregate.MCPEnvMappingSameName,
			resolution: aggregate.MCPEnvResolutionUnavailable,
			wantError:  "requires host-runtime resolution",
		},
		{
			name:       "aliased without resolution",
			mapping:    aggregate.MCPEnvMappingAliased,
			resolution: aggregate.MCPEnvResolutionUnavailable,
			wantError:  "requires host-runtime resolution",
		},
		{
			name:       "unknown mapping",
			mapping:    "renamed_by_shell",
			resolution: aggregate.MCPEnvResolutionHostRuntime,
			wantError:  "unsupported MCP environment-reference mapping",
		},
		{
			name:       "unknown resolution",
			mapping:    aggregate.MCPEnvMappingAliased,
			resolution: "apply_time",
			wantError:  "requires host-runtime resolution",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract, err := aggregate.NewMCPEnvReferenceContract(test.mapping, test.resolution)
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("NewMCPEnvReferenceContract error = %v, want containing %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewMCPEnvReferenceContract returned error: %v", err)
			}
			if contract.Mapping() != test.mapping || contract.Resolution() != test.resolution {
				t.Fatalf("contract = %#v, want mapping=%q resolution=%q", contract, test.mapping, test.resolution)
			}
		})
	}
}

func TestZeroMCPEnvReferenceContractDoesNotClaimSupport(t *testing.T) {
	var contract aggregate.MCPEnvReferenceContract
	if contract.Supported() {
		t.Fatal("zero MCPEnvReferenceContract claimed environment-reference support")
	}
	if contract.AdmitsReference("TOKEN", "TOKEN") {
		t.Fatal("zero MCPEnvReferenceContract admitted an environment reference")
	}
}

func TestMCPEnvReferenceContractOwnsMappingRepresentability(t *testing.T) {
	tests := []struct {
		name       string
		mapping    aggregate.MCPEnvReferenceMapping
		resolution aggregate.MCPEnvReferenceResolution
		childName  string
		sourceName string
		want       bool
	}{
		{
			name:       "unsupported rejects same name",
			mapping:    aggregate.MCPEnvMappingUnsupported,
			resolution: aggregate.MCPEnvResolutionUnavailable,
			childName:  "TOKEN",
			sourceName: "TOKEN",
		},
		{
			name:       "same name accepts same name",
			mapping:    aggregate.MCPEnvMappingSameName,
			resolution: aggregate.MCPEnvResolutionHostRuntime,
			childName:  "TOKEN",
			sourceName: "TOKEN",
			want:       true,
		},
		{
			name:       "same name rejects alias",
			mapping:    aggregate.MCPEnvMappingSameName,
			resolution: aggregate.MCPEnvResolutionHostRuntime,
			childName:  "CHILD_TOKEN",
			sourceName: "HOST_TOKEN",
		},
		{
			name:       "aliased accepts alias",
			mapping:    aggregate.MCPEnvMappingAliased,
			resolution: aggregate.MCPEnvResolutionHostRuntime,
			childName:  "CHILD_TOKEN",
			sourceName: "HOST_TOKEN",
			want:       true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract, err := aggregate.NewMCPEnvReferenceContract(test.mapping, test.resolution)
			if err != nil {
				t.Fatalf("NewMCPEnvReferenceContract returned error: %v", err)
			}
			if got := contract.AdmitsReference(test.childName, test.sourceName); got != test.want {
				t.Fatalf("AdmitsReference(%q, %q) = %t, want %t", test.childName, test.sourceName, got, test.want)
			}
		})
	}
}

func TestMCPPlacementAdmitEnvironmentReferenceUsesContract(t *testing.T) {
	sameNamePlacement, err := aggregate.NewMCPPlacement(aggregate.MCPPlacementInput{
		ID:                     "test.project.same-name-env",
		Target:                 target.TargetCodex,
		Scope:                  target.ScopeProject,
		ConfigLayer:            "test-project-same-name-env",
		ConfigPath:             ".test/config.toml",
		MergeUnit:              aggregate.MCPMergeUnitServerEntry,
		ContentPathPrefix:      "/mcp_servers",
		SiblingRetention:       aggregate.MCPSiblingRetentionPreserveUnmanaged,
		CodecContractID:        "test-project-same-name-env-v1",
		ComparedFields:         []string{"command"},
		Absence:                aggregate.MCPAbsenceRemoveBinding,
		EnvReferenceMapping:    aggregate.MCPEnvMappingSameName,
		EnvReferenceResolution: aggregate.MCPEnvResolutionHostRuntime,
	})
	if err != nil {
		t.Fatalf("NewMCPPlacement returned error: %v", err)
	}
	if err := sameNamePlacement.AdmitEnvironmentReference("TOKEN", "TOKEN"); err != nil {
		t.Fatalf("same-name reference was rejected: %v", err)
	}
	if err := sameNamePlacement.AdmitEnvironmentReference("CHILD_TOKEN", "HOST_TOKEN"); err == nil ||
		!strings.Contains(err.Error(), "supports only same-name environment references") {
		t.Fatalf("aliased reference error = %v", err)
	} else {
		var failure aggregate.MCPEnvReferenceAdmissionError
		if !errors.As(err, &failure) ||
			failure.PlacementID() != sameNamePlacement.ID() ||
			failure.Mapping() != aggregate.MCPEnvMappingSameName {
			t.Fatalf("aliased reference error = %#v, want typed same-name admission error", err)
		}
	}

	unsupported, ok := aggregate.ImplementedMCPPlacement(target.TargetCodex, target.ScopeProject)
	if !ok {
		t.Fatal("Codex project placement missing")
	}
	if err := unsupported.AdmitEnvironmentReference("TOKEN", "TOKEN"); err == nil ||
		!strings.Contains(err.Error(), "does not support environment references") {
		t.Fatalf("unsupported reference error = %v", err)
	}
}
