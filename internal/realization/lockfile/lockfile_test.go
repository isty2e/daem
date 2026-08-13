package lockfile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	resourcetopology "github.com/isty2e/daem/internal/topology/resource"
)

func TestMarshalAndLoadExactSupplyLockfile(t *testing.T) {
	contract := directSkillSubjectContract(t, "oracle")
	file := lockfileWithSubjects(t, contract)

	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	rendered := string(content)
	assertInOrder(t, rendered, []string{
		currentLockfileVersionEnvelope(),
		"[[locked.subject]]",
		`entity_id = "skill:oracle"`,
		`subject_id = "resource/skill/oracle"`,
		`ownership = "manifest"`,
		`on_absent = "apply"`,
		"[locked.subject.exact_supply]",
		`source_id = "local:skills/oracle?mode=vendor"`,
		`kind = "directory"`,
		"[locked.subject.derivation]",
		"[locked.subject.derivation.direct_resolution]",
	})
	for _, legacy := range []string{"[[locked.skill]]", "[[locked.hook]]", "[[locked.instructions]]", "declaration =", "skill_group_index"} {
		if strings.Contains(rendered, legacy) {
			t.Fatalf("rendered current lockfile contains legacy field %q:\n%s", legacy, rendered)
		}
	}

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.Version != lock.CurrentVersion {
		t.Fatalf("loaded version = %d", loaded.Version)
	}
	assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())
}

func TestMarshalAndLoadEmptyLockedSection(t *testing.T) {
	file := lockfileWithSubjects(t)
	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	loaded, err := Load(t.Context(), writeLockfileText(t, string(content)))
	if err != nil {
		t.Fatalf("Load returned error: %v\ncontent:\n%s", err, content)
	}
	if loaded.Locked.Len() != 0 {
		t.Fatalf("loaded subject count = %d, want 0", loaded.Locked.Len())
	}
}

func TestReplayCoverageDTOUsesCanonicalAbsentEmptyExclusions(t *testing.T) {
	coverage, err := lock.NewReplayCoverage(
		lock.ReplayExact,
		lock.ReplayExact,
		lock.ReplayNotApplicable,
		nil,
	)
	if err != nil {
		t.Fatalf("NewReplayCoverage returned error: %v", err)
	}
	if exclusions := replayCoverageToDTO(coverage).Exclusions; exclusions != nil {
		t.Fatalf("encoded exclusions = %#v, want nil canonical omission", exclusions)
	}
}

func TestLoadReencodesCanonicalV5BytesDeterministically(t *testing.T) {
	file := lockfileWithSubjects(
		t,
		claudeProjectMCPSubjectContract(t),
		directSkillSubjectContract(t, "review"),
		repairedSkillSubjectContract(t),
	)
	first, err := Marshal(file)
	if err != nil {
		t.Fatalf("first Marshal returned error: %v", err)
	}
	loaded, err := Load(t.Context(), writeLockfileText(t, string(first)))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	second, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("second Marshal returned error: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("lockfile bytes changed after canonical round trip:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestLoadAcceptsCRLFAndReencodesCanonicalLF(t *testing.T) {
	file := lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle"))
	canonical, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	crlf := strings.ReplaceAll(string(canonical), "\n", "\r\n")
	loaded, err := Load(t.Context(), writeLockfileText(t, crlf))
	if err != nil {
		t.Fatalf("Load returned error for valid CRLF TOML: %v", err)
	}
	reencoded, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal after CRLF load returned error: %v", err)
	}
	if !bytes.Equal(reencoded, canonical) {
		t.Fatalf("CRLF input did not canonicalize to LF bytes:\nwant:\n%s\ngot:\n%s", canonical, reencoded)
	}
}

func TestLoadAcceptsUTF8BOMAndReencodesWithoutIt(t *testing.T) {
	file := lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle"))
	canonical, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	loaded, err := Load(t.Context(), writeLockfileText(t, "\uFEFF"+string(canonical)))
	if err != nil {
		t.Fatalf("Load returned error for UTF-8 BOM input: %v", err)
	}
	reencoded, err := Marshal(loaded)
	if err != nil {
		t.Fatalf("Marshal after BOM load returned error: %v", err)
	}
	if !bytes.Equal(reencoded, canonical) {
		t.Fatalf("BOM input did not canonicalize to BOM-free bytes:\nwant:\n%s\ngot:\n%s", canonical, reencoded)
	}
}

func TestMarshalAndLoadRepairedSkillLockfile(t *testing.T) {
	contract := repairedSkillSubjectContract(t)
	file := lockfileWithSubjects(t, contract)

	content, err := Marshal(file)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	rendered := string(content)
	assertInOrder(t, rendered, []string{
		"[[locked.subject]]",
		`entity_id = "skill:oracle"`,
		"[locked.subject.exact_supply]",
		"[locked.subject.derivation]",
		"[locked.subject.derivation.deterministic_transform]",
		`algorithm_id = "compat.skill.repair"`,
		"[locked.subject.repair_recipe]",
		"[[locked.subject.repair_recipe.operation]]",
		`kind = "rename"`,
		"[[locked.subject.repair_recipe.operation]]",
		`kind = "set_frontmatter_string"`,
	})

	loaded, err := Load(t.Context(), writeLockfileText(t, rendered))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	assertLockedSubjectsEqual(t, loaded.Locked.Subjects(), file.Locked.Subjects())
	loadedRecipe, ok := loaded.Locked.Subjects()[0].RepairRecipe()
	if !ok || len(loadedRecipe.Operations()) != 2 {
		t.Fatalf("loaded repair recipe = %#v, present=%t", loadedRecipe, ok)
	}
}

func TestMarshalRejectsUnsupportedSnapshotVersion(t *testing.T) {
	file := lockfileWithSubjects(t, directSkillSubjectContract(t, "oracle"))
	file.Version = 1

	_, err := Marshal(file)
	if err == nil {
		t.Fatal("Marshal returned nil error")
	}
	if !strings.Contains(err.Error(), "unsupported lockfile version 1") {
		t.Fatalf("error = %q, want unsupported version validation", err)
	}
}

func directSkillSubjectContract(t *testing.T, name string) lock.LockedSubjectContract {
	t.Helper()
	identity := exactDirectoryIdentity(t, name, artifact.HashFileContent([]byte("exact "+name)))
	derivation, err := lock.NewDirectResolutionDerivation(identity)
	if err != nil {
		t.Fatalf("NewDirectResolutionDerivation returned error: %v", err)
	}
	entityID := desiredEntityID(t, entity.KindSkill, name)
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	contract, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:    entityID,
		SubjectID:   subjectID,
		ExactSupply: identity,
		Derivation:  derivation,
	})
	if err != nil {
		t.Fatalf("NewExactSupplySubjectContract returned error: %v", err)
	}
	return contract
}

func repairedSkillSubjectContract(t *testing.T) lock.LockedSubjectContract {
	t.Helper()
	sourceID := artifact.SourceID("git:locator=https%3A%2F%2Fexample.com%2Frepo.git&path=skills%2Foracle&ref=name%3Amain")
	resolvedRef := artifact.ResolvedRef("abc123")
	input := exactIdentity(t, sourceID, resolvedRef, artifact.HashFileContent([]byte("original tree")))
	output := exactIdentity(t, sourceID, resolvedRef, artifact.HashFileContent([]byte("repaired tree")))
	originalFileHash := artifact.HashFileContent([]byte("---\ndescription: Demo\n---\n"))
	repairedFileHash := artifact.HashFileContent([]byte("---\nname: oracle\ndescription: Demo\n---\n"))
	rename, err := skillrepair.NewRenameOperation("skill.md", "SKILL.md", originalFileHash, 0o600)
	if err != nil {
		t.Fatalf("NewRenameOperation returned error: %v", err)
	}
	setName, err := skillrepair.NewSetFrontmatterStringOperation(
		"SKILL.md", "name", nil, "oracle", 4, nil, []byte("name: oracle\n"), originalFileHash, repairedFileHash,
	)
	if err != nil {
		t.Fatalf("NewSetFrontmatterStringOperation returned error: %v", err)
	}
	recipe, err := skillrepair.NewRecipe(input, output, []skillrepair.Operation{rename, setName})
	if err != nil {
		t.Fatalf("NewRecipe returned error: %v", err)
	}
	derivation, err := lock.NewDeterministicTransformDerivation(lock.DeterministicTransform{
		InputIdentity:          input,
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            skillrepair.DerivationAlgorithmID,
		AlgorithmVersion:       fmt.Sprintf("v%d", recipe.Version()),
		ExecutionDomain:        skillrepair.DerivationExecutionDomain,
		ExpectedOutputIdentity: output,
	})
	if err != nil {
		t.Fatalf("NewDeterministicTransformDerivation returned error: %v", err)
	}
	entityID := desiredEntityID(t, entity.KindSkill, "oracle")
	subjectID, err := resourcetopology.Subject(entityID)
	if err != nil {
		t.Fatalf("resource topology subject: %v", err)
	}
	contract, err := lock.NewExactSupplySubjectContract(lock.ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    subjectID,
		ExactSupply:  output,
		Derivation:   derivation,
		RepairRecipe: &recipe,
	})
	if err != nil {
		t.Fatalf("NewExactSupplySubjectContract returned error: %v", err)
	}
	return contract
}

func exactDirectoryIdentity(t *testing.T, name string, hash artifact.ContentHash) artifact.ExactIdentity {
	t.Helper()
	return exactIdentity(t, artifact.SourceID("local:skills/"+name+"?mode=vendor"), "", hash)
}

func exactIdentity(
	t *testing.T,
	sourceID artifact.SourceID,
	resolvedRef artifact.ResolvedRef,
	hash artifact.ContentHash,
) artifact.ExactIdentity {
	t.Helper()
	identity, err := artifact.NewExactIdentity(sourceID, resolvedRef, artifact.ArtifactKindDirectory, hash)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	return identity
}

func desiredEntityID(t *testing.T, kind entity.Kind, name string) entity.ID {
	t.Helper()
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	return id
}

func lockfileWithSubjects(
	t *testing.T,
	subjects ...lock.LockedSubjectContract,
) lock.File {
	t.Helper()
	locked, err := lock.NewLockedSection(subjects, nil)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	return lock.File{Version: lock.CurrentVersion, Locked: locked}
}

func assertLockedSubjectsEqual(
	t *testing.T,
	got []lock.LockedSubjectContract,
	want []lock.LockedSubjectContract,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("locked subject count = %d, want %d", len(got), len(want))
	}
	for index := range got {
		if !got[index].Equal(want[index]) {
			t.Fatalf("locked subject[%d] did not round trip", index)
		}
	}
}

func writeLockfileText(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "daem.lock.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	return path
}

func currentLockfileVersionEnvelope() string {
	return fmt.Sprintf("version = %d", lock.CurrentVersion)
}

func assertInOrder(t *testing.T, content string, fragments []string) {
	t.Helper()
	offset := 0
	for _, fragment := range fragments {
		index := strings.Index(content[offset:], fragment)
		if index < 0 {
			t.Fatalf("content = %q, missing %q after offset %d", content, fragment, offset)
		}
		offset += index + len(fragment)
	}
}
