//go:build darwin

package ownership

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	outputownership "github.com/isty2e/daem/internal/output/ownership"
)

func TestRejectLegacyOwnershipAddressDiagnosesInjectedSensitiveAuthority(t *testing.T) {
	physicalPath := filepath.Join(t.TempDir(), "ManagedOutput.json")
	currentPath, err := mutation.CanonicalDirectoryEntryPath(physicalPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := outputownership.NewManagedAddress(currentPath, "/mcp/alpha")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := outputownership.NewManagedAddress(strings.ToLower(currentPath), "/mcp/alpha")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New("/tmp/daem-state.json", "/tmp/daem.toml")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := outputownership.NewActiveClaim(legacy, owner)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := outputownership.NewRegistry([]outputownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}

	err = rejectLegacyOwnershipAddressWith(
		registry,
		current,
		func(persistedKey string) error {
			if persistedKey != legacy.Path() {
				t.Fatalf("legacy validator key = %q", persistedKey)
			}
			return fmt.Errorf("see docs/troubleshooting.md#legacy-darwin-path-authority")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "legacy-darwin-path-authority") {
		t.Fatalf("legacy ownership address error = %v", err)
	}
}

func TestRejectLegacyOwnershipAddressRejectsAmbiguousForeignOwner(t *testing.T) {
	physicalPath := filepath.Join(t.TempDir(), "ManagedOutput.json")
	currentPath, err := mutation.CanonicalDirectoryEntryPath(physicalPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := outputownership.NewManagedAddress(currentPath, "")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := outputownership.NewManagedAddress(strings.ToLower(currentPath), "")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := stateauthority.New("/tmp/foreign-state.json", "/tmp/foreign.toml")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := outputownership.NewActiveClaim(legacy, foreign)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := outputownership.NewRegistry([]outputownership.Claim{claim})
	if err != nil {
		t.Fatal(err)
	}

	err = rejectLegacyOwnershipAddressWith(
		registry,
		current,
		func(persistedKey string) error {
			if persistedKey != legacy.Path() {
				t.Fatalf("legacy validator key = %q", persistedKey)
			}
			return fmt.Errorf("ambiguous foreign legacy authority")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "ambiguous foreign legacy authority") {
		t.Fatalf("foreign legacy address error = %v", err)
	}
}
