package archguard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDelegatedRelationContractsCannotRegainModelOnlyPackages(t *testing.T) {
	root := findRepoRoot(t)
	for _, packagePath := range []string{
		filepath.Join("internal", "lifecycle", "delegateattempt"),
		filepath.Join("internal", "lifecycle", "route"),
		filepath.Join("internal", "reconcile", "routepolicy"),
	} {
		files, err := filepath.Glob(filepath.Join(root, packagePath, "*.go"))
		if err != nil {
			t.Fatalf("inspect retired delegated-relation package %q: %v", packagePath, err)
		}
		if len(files) != 0 {
			t.Errorf("retired delegated-relation model package %q contains Go files: %v", packagePath, files)
		}
	}
}

func TestDelegatedRelationGenericKernelsRemainHostIndependent(t *testing.T) {
	root := findRepoRoot(t)
	for _, relativePath := range []string{
		"internal/effect/execute/host_attempt_state.go",
		"internal/assurance/durable/attempt/attempt_history.go",
		"internal/assurance/durable/attempt/delegate_attempt.go",
		"internal/assurance/durable/attempt/host_route_attempt.go",
		"internal/assurance/durable/carrier/global_carrier_claims.go",
		"internal/assurance/durable/carrier/managed_carrier.go",
		"internal/assurance/durable/snapshot_carrier.go",
		"internal/assurance/durable/snapshot_history.go",
		"internal/assurance/observe/relation/batch.go",
		"internal/assurance/observe/relation/model.go",
		"internal/assurance/observe/relation/summary.go",
		"internal/assurance/observe/relation/host/observe.go",
		"internal/reconcile/relation_action.go",
		"internal/cli/present/host_route_attempt.go",
		"internal/cli/present/relation_action.go",
		"internal/assurance/statefile/v8.go",
		"internal/workflow/apply/host_route_attempt.go",
		"internal/workflow/apply/host_route_observer.go",
		"internal/effect/execute/hostroute/build.go",
		"internal/effect/execute/hostroute/model.go",
		"internal/assurance/hostroute/attempt.go",
		"internal/assurance/hostroute/result.go",
		"internal/assurance/hostroute/result_summary.go",
		"internal/reconcile/build/hostroute/relation.go",
		"internal/workflow/readiness/relation.go",
	} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relativePath)))
		if err != nil {
			t.Fatalf("read generic delegated-relation kernel %q: %v", relativePath, err)
		}
		references, err := genericHostReferenceDescriptors(relativePath, content)
		if err != nil {
			t.Fatalf("inspect generic delegated-relation kernel %q: %v", relativePath, err)
		}
		if len(references) != 0 {
			t.Errorf("generic delegated-relation kernel %q contains host-specific references: %v", relativePath, references)
		}
	}
}
