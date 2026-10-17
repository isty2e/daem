package adopt

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologymcp "github.com/isty2e/daem/internal/topology/mcp"
)

func mergeEntityForTest(t *testing.T, kind entity.Kind, name string) MergeResource {
	t.Helper()
	resource, err := NewEntityMergeResource(kind, name)
	if err != nil {
		t.Fatal(err)
	}
	return resource
}

func TestMergeResourceRetainsCanonicalEntityFacts(t *testing.T) {
	const name = "instructions/mcp_server:%25"
	for _, kind := range []entity.Kind{
		entity.KindInstructions, entity.KindSkill, entity.KindHook,
		entity.KindHookAsset, entity.KindMCPServer, entity.KindExtension,
	} {
		t.Run(string(kind), func(t *testing.T) {
			resource := mergeEntityForTest(t, kind, name)
			id, present := resource.EntityID()
			if !present || id.Kind() != kind || id.Name() != name || resource.IsManifest() {
				t.Fatalf("entity facts lost: %#v", resource)
			}
			if err := resource.Validate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMergeResourceRequiresExplicitValidVariant(t *testing.T) {
	for _, test := range []struct {
		kind entity.Kind
		name string
	}{
		{"", "name"},
		{"unknown", "name"},
		{entity.KindSkill, ""},
		{entity.KindSkill, "\xff"},
	} {
		if _, err := NewEntityMergeResource(test.kind, test.name); err == nil {
			t.Fatalf("accepted invalid entity %q/%q", test.kind, test.name)
		}
	}
	if err := (MergeResource{}).Validate(); err == nil {
		t.Fatal("missing resource became a valid manifest decision")
	}
	if err := (MergeResult{Status: MergeStatusNoop, Detail: "unchanged"}).Validate(); err == nil {
		t.Fatal("merge result accepted a missing resource")
	}
	manifest := ManifestMergeResource()
	if err := manifest.Validate(); err != nil {
		t.Fatal(err)
	}
	if id, present := manifest.EntityID(); present || id != (entity.ID{}) || !manifest.IsManifest() {
		t.Fatalf("manifest acquired entity facts: %#v", manifest)
	}
}

func TestMergePlanIdentityRetainsEveryDecisionFact(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "daem.toml")
	sourceDirectory, err := NewSourceDirectory(output, filepath.Join(root, "daem.d"))
	if err != nil {
		t.Fatal(err)
	}
	request, err := NewRequest([]target.Target{target.TargetCodex}, []target.Scope{target.ScopeProject}, output, sourceDirectory, true)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := NewCandidateSet(CandidateSetInput{})
	if err != nil {
		t.Fatal(err)
	}
	identity := func(result MergeResult) []byte {
		t.Helper()
		plan, err := NewPlan(request, []byte("version = 1\n"), []byte("version = 1\n"), candidates, []MergeResult{result})
		if err != nil {
			t.Fatal(err)
		}
		value, err := plan.IdentityBytes()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	subjectFor := func(scope target.Scope) topology.SubjectID {
		t.Helper()
		subject, err := topologymcp.ProjectionSubject(target.TargetCodex, scope, "context7")
		if err != nil {
			t.Fatal(err)
		}
		return subject
	}
	base := MergeResult{
		Resource: mergeEntityForTest(t, entity.KindMCPServer, "context7"),
		Subject:  subjectFor(target.ScopeProject), Status: MergeStatusNoop, Detail: "already declared",
	}
	before := identity(base)
	if !bytes.Equal(before, identity(base)) {
		t.Fatal("same plan has unstable identity")
	}
	for _, test := range []struct {
		name   string
		change func(*MergeResult)
	}{
		{"kind", func(result *MergeResult) {
			result.Resource = mergeEntityForTest(t, entity.KindInstructions, "context7")
		}},
		{"name", func(result *MergeResult) { result.Resource = mergeEntityForTest(t, entity.KindMCPServer, "other") }},
		{"manifest", func(result *MergeResult) { result.Resource = ManifestMergeResource() }},
		{"subject", func(result *MergeResult) { result.Subject = subjectFor(target.ScopeGlobal) }},
		{"no-subject", func(result *MergeResult) { result.Subject = topology.SubjectID{} }},
		{"status", func(result *MergeResult) { result.Status = MergeStatusConflict }},
		{"detail", func(result *MergeResult) { result.Detail = "different payload" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.change(&changed)
			if bytes.Equal(before, identity(changed)) {
				t.Fatalf("plan identity ignored %s", test.name)
			}
		})
	}
}
