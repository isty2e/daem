package lock

import (
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func testSkillRepairRecipe(t *testing.T) skillrepair.Recipe {
	t.Helper()
	fileHash := artifact.HashFileContent([]byte("old"))
	replacedHash := artifact.HashFileContent([]byte("new"))
	frontmatterHash := artifact.HashFileContent([]byte("after"))
	rename, err := skillrepair.NewRenameOperation("skill.md", "SKILL.md", fileHash, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	replace, err := skillrepair.NewReplaceBytesOperation(
		"SKILL.md", 0, []byte("old"), []byte("new"), fileHash, replacedHash,
	)
	if err != nil {
		t.Fatal(err)
	}
	oldValue := "before"
	set, err := skillrepair.NewSetFrontmatterStringOperation(
		"SKILL.md", "name", &oldValue, "after", 0,
		[]byte("name: before"), []byte("name: after"), replacedHash, frontmatterHash,
	)
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := skillrepair.NewRecipe(
		mustSkillIdentity(t, "input"),
		mustSkillIdentity(t, "output"),
		[]skillrepair.Operation{rename, replace, set},
	)
	if err != nil {
		t.Fatal(err)
	}
	return recipe
}

func mustSkillIdentity(t *testing.T, content string) artifact.ExactIdentity {
	t.Helper()
	identity, err := artifact.NewExactIdentity(
		"local:skill",
		"revision",
		artifact.ArtifactKindDirectory,
		artifact.HashFileContent([]byte(content)),
	)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func testSkillSet(t *testing.T, setSource source.Source, include []string) desiredskill.SkillSet {
	t.Helper()
	includes := make([]desiredskill.Selector, 0, len(include))
	for _, expression := range include {
		includes = append(includes, testfixture.Selector(t, expression))
	}
	return testfixture.SkillSet(t, desiredskill.SkillSetSpec{
		Source:      setSource,
		Include:     includes,
		Targets:     []target.Target{target.TargetCodex},
		Scope:       target.ScopeProject,
		InstallMode: desiredskill.InstallModeCopy,
		Portable:    true,
	})
}

func mustTopologySubjectID(
	t *testing.T,
	kind topology.SubjectKind,
	namespace string,
	key string,
) topology.SubjectID {
	t.Helper()
	id, err := topology.NewSubjectID(kind, namespace, key)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustResourceSubjectID(t *testing.T, id entity.ID) topology.SubjectID {
	t.Helper()
	subject, err := resourcetopology.Subject(id)
	if err != nil {
		t.Fatal(err)
	}
	return subject
}
