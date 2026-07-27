package build

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/instructions"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	hookcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	"github.com/isty2e/daem/internal/target"
)

func buildWithTestOptions(
	ctx context.Context,
	environment desired.Environment,
	resolver acquisition.Resolver,
	options Options,
) (lock.File, error) {
	options.HookContributionEncoder = hookcodec.CanonicalHookContribution
	options.MCPContributionEncoder = mcpcodec.CanonicalMCPBindingContribution
	return BuildWithOptions(ctx, environment, resolver, options)
}

func lockEnvironment(t *testing.T, spec desired.Spec) desired.Environment {
	t.Helper()
	if len(spec.Targets) == 0 {
		spec.Targets = target.SupportedTargets()
	}
	spec.Defaults = desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy)
	return desiredtest.Environment(t, spec)
}

func projectCopySkill(
	t *testing.T,
	name string,
	sourceSpec source.Source,
	targets []target.Target,
	compatRepair bool,
) skill.Skill {
	t.Helper()
	return desiredtest.Skill(t, skill.Spec{
		Name: name, Source: sourceSpec, Targets: targets,
		Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		CompatRepair: compatRepair,
	})
}

func projectCopySkillSet(
	t *testing.T,
	sourceSpec source.Source,
	selectors []skill.Selector,
	targets []target.Target,
	compatRepair bool,
) skill.SkillSet {
	t.Helper()
	return desiredtest.SkillSet(t, skill.SkillSetSpec{
		Source: sourceSpec, Include: selectors, Targets: targets,
		Scope: target.ScopeProject, InstallMode: skill.InstallModeCopy,
		CompatRepair: compatRepair,
	})
}

func projectInstructions(
	t *testing.T,
	name string,
	sourceSpec source.Source,
	targets []target.Target,
) instructions.Instructions {
	t.Helper()
	return desiredtest.Instructions(t, instructions.Spec{
		Name: name, Source: sourceSpec, Targets: targets, Scope: target.ScopeProject,
	})
}

func mustEntityID(t *testing.T, kind entity.Kind, name string) entity.ID {
	t.Helper()
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	return id
}

func mustGitSource(t *testing.T, locator string, repositoryPath string, ref string) source.Source {
	t.Helper()

	sourceSpec, err := source.NewGitSource(locator, repositoryPath, ref)
	if err != nil {
		t.Fatalf("NewGitSource returned error: %v", err)
	}
	return sourceSpec
}

func mustSourceID(t *testing.T, sourceSpec source.Source) string {
	t.Helper()

	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		t.Fatalf("SourceIDFor returned error: %v", err)
	}
	return string(sourceID)
}

func mustRootListing(
	t *testing.T,
	root source.Source,
	resolvedRef artifact.ResolvedRef,
	kind artifact.ArtifactKind,
	childNames []string,
) source.RootListing {
	t.Helper()

	listing, err := source.NewRootListing(root, resolvedRef, kind, childNames)
	if err != nil {
		t.Fatalf("NewRootListing returned error: %v", err)
	}
	return listing
}

type stubResolver struct {
	artifacts map[string]resolutionFixture
}

// resolutionFixture is pre-normalized test input. Stub resolvers immediately
// convert it through the same canonical constructors as production adapters.
type resolutionFixture struct {
	SourceID    artifact.SourceID
	ContentPath string
	Kind        artifact.ArtifactKind
	ContentHash artifact.ContentHash
	ResolvedRef artifact.ResolvedRef
}

func (resolver stubResolver) Resolve(
	ctx context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	resolvedArtifact, ok := resolver.artifacts[string(sourceID)]
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf("missing source %s", sourceID)
	}

	return resolutionFromTestFixture(ctx, sourceSpec, resolvedArtifact)
}

type rootListingResolver struct {
	root      source.RootListing
	artifacts map[string]resolutionFixture
	resolved  []string
}

func (resolver *rootListingResolver) ListSourceRoot(
	_ context.Context,
	_ source.Source,
	_ acquisition.OperationOptions,
) (source.RootListing, error) {
	return resolver.root, nil
}

func (resolver *rootListingResolver) Resolve(
	ctx context.Context,
	sourceSpec source.Source,
	_ acquisition.OperationOptions,
) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}

	resolver.resolved = append(resolver.resolved, string(sourceID))
	resolvedArtifact, ok := resolver.artifacts[string(sourceID)]
	if !ok {
		return acquisition.Resolution{}, fmt.Errorf("missing source %s", sourceID)
	}

	return resolutionFromTestFixture(ctx, sourceSpec, resolvedArtifact)
}

func resolutionFromTestFixture(
	ctx context.Context,
	sourceSpec source.Source,
	fixture resolutionFixture,
) (acquisition.Resolution, error) {
	sourceID, err := source.SourceIDFor(sourceSpec)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	if fixture.SourceID != "" && fixture.SourceID != sourceID {
		return acquisition.Resolution{}, fmt.Errorf("fixture source id %q does not match request %q", fixture.SourceID, sourceID)
	}
	view, err := access.OpenView(fixture.ContentPath)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	contentHash, err := view.Hash(ctx)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	identity, err := artifact.NewExactIdentity(sourceID, fixture.ResolvedRef, view.Kind(), contentHash)
	if err != nil {
		return acquisition.Resolution{}, err
	}
	return acquisition.NewResolution(sourceSpec, identity, view)
}

func writeSkill(t *testing.T, root string, relativePath string) string {
	t.Helper()

	return writeSkillContent(t, root, relativePath, "---\nname: demo\ndescription: Test skill fixture.\n---\n")
}

func writeSkillContent(t *testing.T, root string, relativePath string, content string) string {
	t.Helper()

	skillPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillPath, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return skillPath
}

func writeLowercaseSkillContent(t *testing.T, root string, relativePath string, content string) string {
	t.Helper()

	skillPath := filepath.Join(root, relativePath)
	if err := os.MkdirAll(skillPath, 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(filepath.Join(skillPath, "skill.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return skillPath
}

func writeFile(t *testing.T, root string, relativePath string, content string) string {
	t.Helper()

	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	return path
}

func contentHashForPath(t *testing.T, path string) artifact.ContentHash {
	t.Helper()

	hash, _, err := access.HashPath(context.Background(), path)
	if err != nil {
		t.Fatalf("HashPath returned error: %v", err)
	}
	return hash
}

func assertRepairOperationKinds(t *testing.T, operations []skillrepair.Operation, expected []string) {
	t.Helper()

	if len(operations) != len(expected) {
		t.Fatalf("operation count = %d, want %d: %#v", len(operations), len(expected), operations)
	}
	for index, operation := range operations {
		if string(operation.Kind()) != expected[index] {
			t.Fatalf("operation[%d].Kind = %q, want %q", index, operation.Kind(), expected[index])
		}
	}
}

func lockedSubjectsOfKind(
	file lock.File,
	kind entity.Kind,
) []lock.LockedSubjectContract {
	result := make([]lock.LockedSubjectContract, 0)
	for _, contract := range file.Locked.Subjects() {
		if contract.EntityID().Kind() == kind {
			result = append(result, contract)
		}
	}
	return result
}

func lockedExactSupplySubjectsOfKind(
	file lock.File,
	kind entity.Kind,
) []lock.LockedSubjectContract {
	result := make([]lock.LockedSubjectContract, 0)
	for _, contract := range file.Locked.Subjects() {
		if contract.EntityID().Kind() != kind {
			continue
		}
		if _, supplied := contract.ExactSupply(); supplied {
			result = append(result, contract)
		}
	}
	return result
}

func lockedPathProjectionSubjectsOfKind(
	file lock.File,
	kind entity.Kind,
) []lock.LockedSubjectContract {
	result := make([]lock.LockedSubjectContract, 0)
	for _, contract := range file.Locked.Subjects() {
		if contract.EntityID().Kind() != kind {
			continue
		}
		realization, realized := contract.Realization()
		if !realized {
			continue
		}
		if _, path := realization.ManagedPathProjection(); path {
			result = append(result, contract)
		}
	}
	return result
}

func mustLockedSubject(
	t *testing.T,
	file lock.File,
	kind entity.Kind,
	name string,
) lock.LockedSubjectContract {
	t.Helper()
	for _, contract := range file.Locked.Subjects() {
		if contract.EntityID().Kind() == kind && contract.EntityID().Name() == name {
			if _, supplied := contract.ExactSupply(); !supplied {
				continue
			}
			return contract
		}
	}
	t.Fatalf("locked subject %s:%s is missing", kind, name)
	return lock.LockedSubjectContract{}
}

func mustExactSupply(
	t *testing.T,
	contract lock.LockedSubjectContract,
) artifact.ExactIdentity {
	t.Helper()
	identity, ok := contract.ExactSupply()
	if !ok {
		t.Fatalf("locked subject %q is missing exact Supply", contract.SubjectID())
	}
	return identity
}

func assertDirectoryEntryMissingExact(t *testing.T, root string, name string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir %q returned error: %v", root, err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			t.Fatalf("directory %q has exact entry %q", root, name)
		}
	}
}

func assertDirectoryEntryExistsExact(t *testing.T, root string, name string) {
	t.Helper()

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir %q returned error: %v", root, err)
	}
	for _, entry := range entries {
		if entry.Name() == name {
			return
		}
	}
	t.Fatalf("directory %q is missing exact entry %q", root, name)
}
