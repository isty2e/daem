package journal

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
)

func TestLoadActivePlanRequiresResolverForRootedAuthority(t *testing.T) {
	paths := journalpublication.Paths{RecoveryDir: filepath.Join(t.TempDir(), "recovery")}
	resolver := func(output.Destination) (string, error) {
		return "/unused", nil
	}
	capability := func(output.Destination, rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, bool, error) {
		return nil, false, nil
	}
	if _, err := LoadActivePlanWithOptions(
		context.Background(),
		paths,
		PlanLoadOptions{Filesystem: journalTestFilesystem(), Resolver: resolver},
	); err == nil || !strings.Contains(err.Error(), "no active recovery journal") {
		t.Fatalf("resolver-only LoadActivePlanWithOptions error = %v, want ordinary plan lookup", err)
	}
	_, err := LoadActivePlanWithOptions(
		context.Background(),
		paths,
		PlanLoadOptions{RootedCapability: capability},
	)
	if err == nil || !strings.Contains(err.Error(), "requires a destination resolver") {
		t.Fatalf("rooted-only LoadActivePlanWithOptions error = %v, want resolver requirement", err)
	}
}

func TestLoadActivePlanRequiresFilesystemBeforeRecoveryObservation(t *testing.T) {
	_, err := LoadActivePlanWithOptions(
		context.Background(),
		journalpublication.Paths{},
		PlanLoadOptions{},
	)
	if err == nil || !strings.Contains(err.Error(), "filesystem is required") {
		t.Fatalf("LoadActivePlanWithOptions error = %v, want filesystem requirement", err)
	}
}
