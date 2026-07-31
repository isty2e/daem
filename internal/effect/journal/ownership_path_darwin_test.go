//go:build darwin

package journal

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/output/ownership"
)

func TestRejectLegacyRecoveryClaimAddressDiagnosesInjectedSensitiveAuthority(t *testing.T) {
	physicalPath := filepath.Join(t.TempDir(), "ManagedOutput.json")
	currentPath, err := mutation.CanonicalDirectoryEntryPath(physicalPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := ownership.NewManagedAddress(currentPath, "/mcp/alpha")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ownership.NewManagedAddress(strings.ToLower(currentPath), "/mcp/alpha")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New("/tmp/daem-state.json", "/tmp/daem.toml")
	if err != nil {
		t.Fatal(err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(legacy, owner, "legacy-address-test")
	if err != nil {
		t.Fatal(err)
	}
	transitions := []ownershipmutation.ClaimTransition{transition}
	remaining := map[string]ownershipmutation.ClaimTransition{
		ownershipAddressKey(legacy): transition,
	}

	err = rejectLegacyRecoveryClaimAddressWith(
		current,
		transitions,
		remaining,
		func(persistedKey string) error {
			if persistedKey != legacy.Path() {
				t.Fatalf("legacy validator key = %q", persistedKey)
			}
			return fmt.Errorf("see docs/troubleshooting.md#legacy-darwin-path-authority")
		},
	)
	if err == nil || !strings.Contains(err.Error(), "legacy-darwin-path-authority") {
		t.Fatalf("legacy recovery address error = %v", err)
	}
}

func TestRejectLegacyRecoveryClaimAddressIgnoresDisjointProjection(t *testing.T) {
	physicalPath := filepath.Join(t.TempDir(), "ManagedOutput.json")
	currentPath, err := mutation.CanonicalDirectoryEntryPath(physicalPath)
	if err != nil {
		t.Fatal(err)
	}
	current, err := ownership.NewManagedAddress(currentPath, "/mcp/alpha")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ownership.NewManagedAddress(strings.ToLower(currentPath), "/mcp/beta")
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New("/tmp/daem-state.json", "/tmp/daem.toml")
	if err != nil {
		t.Fatal(err)
	}
	transition, err := ownershipmutation.NewAcquireTransition(legacy, owner, "disjoint-address-test")
	if err != nil {
		t.Fatal(err)
	}
	transitions := []ownershipmutation.ClaimTransition{transition}
	remaining := map[string]ownershipmutation.ClaimTransition{
		ownershipAddressKey(legacy): transition,
	}

	if err := rejectLegacyRecoveryClaimAddressWith(
		current,
		transitions,
		remaining,
		func(string) error {
			t.Fatal("disjoint projection reached legacy validator")
			return nil
		},
	); err != nil {
		t.Fatalf("disjoint legacy address returned error: %v", err)
	}
}
