package mutation

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestPhysicalAuthorityBatchMatchesIndependentDomains(t *testing.T) {
	root := t.TempDir()
	requests := []PhysicalAuthorityRequest{
		{Path: root, Target: "codex", Scope: "project"},
		{Path: filepath.Join(root, "Future", "one"), Target: "codex", Scope: "project"},
		{Path: filepath.Join(root, "Future", "two"), Target: "claude-code", Scope: "project"},
		{Path: filepath.Join(root, "Future", "one"), Target: "claude-code", Scope: "global"},
	}
	batch, err := NewPhysicalAuthoritySet(requests...)
	if err != nil {
		t.Fatal(err)
	}
	var want []Domain
	for _, request := range requests {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			domain, err := NewPhysicalPathDomain(PhysicalPathRequest{
				Path: request.Path, Access: AccessExclusive, Effect: effect,
				Target: request.Target, Scope: request.Scope,
			})
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, domain)
		}
	}
	if !reflect.DeepEqual(batch.domains, want) {
		t.Fatalf("batch domains = %#v; want %#v", batch.domains, want)
	}
}
