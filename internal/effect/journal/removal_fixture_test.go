//go:build darwin || linux

package journal

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
)

func removalPlanTestIntent(t *testing.T, parent string, content []byte) recovery.RemovalIntent {
	t.Helper()
	parentProvenance := removalPlanTestProvenance(t, parent)
	names, err := mutationfs.NewLogicalRemovalNames(".daem-tombstone-0123456789abcdef0123456789abcdef", ".daem-cleanup-0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("construct removal names: %v", err)
	}
	namespace, err := recovery.NewExistingParentAuthority(parentProvenance, names)
	if err != nil {
		t.Fatalf("construct removal namespace: %v", err)
	}
	state, err := recovery.NewExpectedRemovalState(recovery.ExpectedPathState{
		Existed: true, Kind: recovery.PathKindFile,
		ContentHash: string(artifact.HashFileContentWithExecutable(content, false)), PathMode: recovery.NewPermissionMode(0o600),
	})
	if err != nil {
		t.Fatalf("construct removal state: %v", err)
	}
	destination, err := output.Parse("managed/config")
	if err != nil {
		t.Fatalf("construct removal destination: %v", err)
	}
	demand, err := recovery.NewRemovalDemand(target.ScopeProject, destination, []recovery.RemovalState{state})
	if err != nil {
		t.Fatalf("construct removal demand: %v", err)
	}
	intent, err := recovery.NewRemovalIntent(demand, namespace)
	if err != nil {
		t.Fatalf("construct removal intent: %v", err)
	}
	return intent
}

func removalPlanTestProvenance(t *testing.T, path string) recovery.RootProvenance {
	t.Helper()
	root, err := rootedpath.CaptureRootNoFollow(path)
	if err != nil {
		t.Fatalf("capture removal root: %v", err)
	}
	authority, authorityErr := root.Authority()
	if authorityErr != nil {
		_ = root.Close()
		t.Fatalf("read removal root authority: %v", authorityErr)
	}
	provenance, provenanceErr := authority.Provenance()
	closeErr := root.Close()
	if provenanceErr != nil || closeErr != nil {
		t.Fatalf("read removal root provenance: provenance=%v close=%v", provenanceErr, closeErr)
	}
	result, err := recovery.NewRootProvenance(provenance.PhysicalRoot(), provenance.ObjectFingerprint(), provenance.MountFingerprint())
	if err != nil {
		t.Fatalf("construct removal root provenance: %v", err)
	}
	return result
}
