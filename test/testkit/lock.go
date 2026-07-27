package testkit

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type LockedExactSupplyView struct {
	Contract    lock.LockedSubjectContract
	Name        string
	SourceID    string
	ResolvedRef string
	ContentHash string
	Repair      *skillrepair.Recipe
}

type LockDeltaFixtureHashes struct {
	Instructions string
	Oracle       string
	Review       string
}

func WriteLockDeltaFixture(t *testing.T, root string) (string, string, LockDeltaFixtureHashes) {
	t.Helper()

	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	WriteFile(t, root, "instructions/AGENTS.md", "instructions\n")
	WriteFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	WriteFile(t, root, "skills/review/SKILL.md", "---\nname: review\ndescription: review\n---\n")
	if err := os.WriteFile(manifestPath, []byte(`
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[[skill]]
name = "review"
source = { path = "skills/review", mode = "vendor" }
targets = ["codex"]
`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	hashes := LockDeltaFixtureHashes{
		Instructions: HashPath(t, filepath.Join(root, "instructions/AGENTS.md")),
		Oracle:       HashDirectory(t, filepath.Join(root, "skills/oracle")),
		Review:       HashDirectory(t, filepath.Join(root, "skills/review")),
	}
	WriteLockfile(t, lockfilePath, ExactSupplyLockfile(
		t,
		ExactSupplyFixture{Kind: ExactSupplySkill, Name: "oracle", SourceID: "local:skills/oracle?mode=vendor", ContentHash: FixtureHash("old-oracle")},
		ExactSupplyFixture{Kind: ExactSupplySkill, Name: "legacy", SourceID: "local:skills/legacy?mode=vendor", ContentHash: FixtureHash("legacy")},
		ExactSupplyFixture{Kind: ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: hashes.Instructions},
	))

	return manifestPath, lockfilePath, hashes
}

func WriteInstructionApplyFixture(t *testing.T, root string) (string, string, string) {
	t.Helper()
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	WriteFile(t, root, "instructions/AGENTS.md", "managed instructions\n")
	instructionHash := HashPath(t, filepath.Join(root, "instructions", "AGENTS.md"))
	WriteFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/AGENTS.md"
`)
	WriteLockfile(t, lockfilePath, ExactSupplyLockfile(t, ExactSupplyFixture{Kind: ExactSupplyInstructions, Name: "project", SourceID: "local:instructions/AGENTS.md?mode=vendor", ContentHash: instructionHash}))
	return manifestPath, lockfilePath, instructionHash
}

func LockedManagedAggregateContribution(
	t testing.TB,
	contract lock.LockedSubjectContract,
) aggregate.ManagedContribution {
	t.Helper()
	realization, ok := contract.Realization()
	if !ok {
		t.Fatalf("locked subject %q is missing realization", contract.SubjectID())
	}
	contribution, ok := realization.ManagedAggregateContribution()
	if !ok {
		t.Fatalf("locked subject %q is not a managed aggregate contribution", contract.SubjectID())
	}
	return contribution
}

func LockedDelegatedRelation(
	t testing.TB,
	contract lock.LockedSubjectContract,
) realization.DelegatedRelation {
	t.Helper()
	return snapshottest.DelegatedRelation(t, contract)
}

func LockedSkills(t testing.TB, file lock.File) []LockedExactSupplyView {
	t.Helper()
	return lockedExactSupplies(t, file, entity.KindSkill)
}

func LockedInstructions(t testing.TB, file lock.File) []LockedExactSupplyView {
	t.Helper()
	return lockedExactSupplies(t, file, entity.KindInstructions)
}

func LockedHooks(t testing.TB, file lock.File) []LockedExactSupplyView {
	t.Helper()
	return lockedExactSupplies(t, file, entity.KindHook)
}

func lockedExactSupplies(
	t testing.TB,
	file lock.File,
	kind entity.Kind,
) []LockedExactSupplyView {
	t.Helper()
	var result []LockedExactSupplyView
	for _, contract := range file.Locked.Subjects() {
		if contract.EntityID().Kind() != kind {
			continue
		}
		identity, ok := contract.ExactSupply()
		if !ok {
			continue
		}
		view := LockedExactSupplyView{
			Contract:    contract,
			Name:        contract.EntityID().Name(),
			SourceID:    string(identity.SourceID()),
			ResolvedRef: string(identity.ResolvedRef()),
			ContentHash: string(identity.ContentHash()),
		}
		if recipe, hasRecipe := contract.RepairRecipe(); hasRecipe {
			view.Repair = &recipe
		}
		result = append(result, view)
	}
	return result
}

type ExactSupplyKind string

const (
	ExactSupplySkill        ExactSupplyKind = "skill"
	ExactSupplyInstructions ExactSupplyKind = "instructions"
)

// ExactSupplyFixture describes one canonical exact-Supply fixture at a CLI boundary.
type ExactSupplyFixture struct {
	Kind        ExactSupplyKind
	Name        string
	SourceID    string
	ResolvedRef string
	ContentHash string
	// Placement fields use Codex/project/copy defaults when omitted. InstallName
	// applies only to directory-backed Skill projections.
	Targets     []target.Target
	Scope       target.Scope
	InstallName string
	InstallMode realization.PathProjectionMode
	// Destinations selects an admitted non-default file placement per target.
	// It applies only to Instructions fixtures.
	Destinations map[target.Target]string
}

// ExactSupplyLockfile constructs a current canonical lockfile for CLI tests.
func ExactSupplyLockfile(t *testing.T, fixtures ...ExactSupplyFixture) lock.File {
	t.Helper()
	contracts := make([]lock.LockedSubjectContract, 0, len(fixtures)*2)
	for _, fixture := range fixtures {
		kind, artifactKind := exactSupplyFixtureKinds(t, fixture.Kind)
		scope := exactSupplyFixtureScope(fixture)
		var exactFileUse *lock.ExactFileUse
		if fixture.Kind == ExactSupplyInstructions {
			fileUse, err := lock.NewExactFileUse(scope, false)
			if err != nil {
				t.Fatalf("NewExactFileUse returned error: %v", err)
			}
			exactFileUse = &fileUse
		}
		contract := snapshottest.ExactSupplyContract(t, snapshottest.ExactSupplyInput{
			Kind:         kind,
			Name:         fixture.Name,
			SourceID:     artifact.SourceID(fixture.SourceID),
			ResolvedRef:  artifact.ResolvedRef(fixture.ResolvedRef),
			ArtifactKind: artifactKind,
			ContentHash:  artifact.ContentHash(fixture.ContentHash),
			ExactFileUse: exactFileUse,
		})
		contracts = append(contracts, contract)
		switch fixture.Kind {
		case ExactSupplySkill:
			contracts = append(contracts, exactSupplySkillProjections(t, fixture)...)
		case ExactSupplyInstructions:
			contracts = append(contracts, exactSupplyInstructionProjections(t, fixture)...)
		}
	}
	return snapshottest.File(t, contracts...)
}

func exactSupplySkillProjections(
	t *testing.T,
	fixture ExactSupplyFixture,
) []lock.LockedSubjectContract {
	t.Helper()
	targets := fixture.Targets
	if len(targets) == 0 {
		targets = []target.Target{target.TargetCodex}
	}
	scope := exactSupplyFixtureScope(fixture)
	installName := fixture.InstallName
	if installName == "" {
		installName = fixture.Name
	}
	mode := fixture.InstallMode
	if mode == "" {
		mode = realization.PathProjectionCopy
	}
	placements, err := profile.ManagedPathPlacementsFor(entity.KindSkill, scope, targets)
	if err != nil {
		t.Fatalf("ManagedPathPlacementsFor returned error: %v", err)
	}
	id, err := entity.New(entity.KindSkill, fixture.Name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	contracts := make([]lock.LockedSubjectContract, 0, len(placements))
	for _, placement := range placements {
		destination, err := placement.ChildDestination(installName)
		if err != nil {
			t.Fatalf("ChildDestination returned error: %v", err)
		}
		writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
		if err != nil {
			t.Fatalf("ManagedPathOperationRoute(write) returned error: %v", err)
		}
		removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
		if err != nil {
			t.Fatalf("ManagedPathOperationRoute(remove) returned error: %v", err)
		}
		spec, err := placement.Realize(destination, mode, writeRoute)
		if err != nil {
			t.Fatalf("Realize returned error: %v", err)
		}
		subject, err := topologyprojection.Subject(id, placement.ID())
		if err != nil {
			t.Fatalf("projection.Subject returned error: %v", err)
		}
		contract, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
			EntityID:      id,
			SubjectID:     subject,
			Realization:   spec,
			WriteRouteID:  writeRoute.RouteID(),
			RemoveRouteID: removeRoute.RouteID(),
		})
		if err != nil {
			t.Fatalf("NewManagedPathSubjectContract returned error: %v", err)
		}
		contracts = append(contracts, contract)
	}
	return contracts
}

func exactSupplyInstructionProjections(
	t *testing.T,
	fixture ExactSupplyFixture,
) []lock.LockedSubjectContract {
	t.Helper()
	targets := fixture.Targets
	if len(targets) == 0 {
		targets = []target.Target{target.TargetCodex}
	}
	mode := fixture.InstallMode
	if mode == "" {
		mode = realization.PathProjectionCopy
	}
	placementsByID := make(map[string]profile.SelectedManagedPathPlacement, len(targets))
	for _, consumer := range targets {
		var placement profile.SelectedManagedPathPlacement
		destination, selected := fixture.Destinations[consumer]
		if selected {
			var err error
			placement, err = profile.ManagedFilePlacementFor(
				entity.KindInstructions,
				consumer,
				exactSupplyFixtureScope(fixture),
				parseDestination(t, destination),
			)
			if err != nil {
				t.Fatalf("ManagedFilePlacementFor returned error: %v", err)
			}
		} else {
			defaults, err := profile.ManagedPathPlacementsFor(
				entity.KindInstructions,
				exactSupplyFixtureScope(fixture),
				[]target.Target{consumer},
			)
			if err != nil {
				t.Fatalf("ManagedPathPlacementsFor returned error: %v", err)
			}
			if len(defaults) != 1 {
				t.Fatalf("default Instructions placements = %d, want 1", len(defaults))
			}
			placement = defaults[0]
		}
		if existing, shared := placementsByID[placement.ID()]; shared {
			merged, err := profile.MergeManagedPathPlacements(existing, placement)
			if err != nil {
				t.Fatalf("MergeManagedPathPlacements returned error: %v", err)
			}
			placement = merged
		}
		placementsByID[placement.ID()] = placement
	}
	placementIDs := make([]string, 0, len(placementsByID))
	for placementID := range placementsByID {
		placementIDs = append(placementIDs, placementID)
	}
	sort.Strings(placementIDs)
	id, err := entity.New(entity.KindInstructions, fixture.Name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	contracts := make([]lock.LockedSubjectContract, 0, len(placementIDs))
	for _, placementID := range placementIDs {
		placement := placementsByID[placementID]
		writeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationWrite)
		if err != nil {
			t.Fatalf("ManagedPathOperationRoute(write) returned error: %v", err)
		}
		removeRoute, err := profile.ManagedPathOperationRoute(placement, profile.OperationRemove)
		if err != nil {
			t.Fatalf("ManagedPathOperationRoute(remove) returned error: %v", err)
		}
		spec, err := placement.Realize(placement.Root(), mode, writeRoute)
		if err != nil {
			t.Fatalf("Realize returned error: %v", err)
		}
		subject, err := topologyprojection.Subject(id, placement.ID())
		if err != nil {
			t.Fatalf("projection.Subject returned error: %v", err)
		}
		contract, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
			EntityID:      id,
			SubjectID:     subject,
			Realization:   spec,
			WriteRouteID:  writeRoute.RouteID(),
			RemoveRouteID: removeRoute.RouteID(),
		})
		if err != nil {
			t.Fatalf("NewManagedPathSubjectContract returned error: %v", err)
		}
		contracts = append(contracts, contract)
	}
	return contracts
}

func exactSupplyFixtureScope(fixture ExactSupplyFixture) target.Scope {
	if fixture.Scope != "" {
		return fixture.Scope
	}
	return target.ScopeProject
}

// FixtureHash returns a valid deterministic hash for synthetic test-only content.
func FixtureHash(value string) string {
	return string(artifact.HashFileContent([]byte(value)))
}

func exactSupplyFixtureKinds(t *testing.T, kind ExactSupplyKind) (entity.Kind, artifact.ArtifactKind) {
	t.Helper()
	switch kind {
	case ExactSupplySkill:
		return entity.KindSkill, artifact.ArtifactKindDirectory
	case ExactSupplyInstructions:
		return entity.KindInstructions, artifact.ArtifactKindFile
	default:
		t.Fatalf("unsupported exact-Supply fixture kind %q", kind)
		return "", ""
	}
}

func WriteLockfile(t *testing.T, path string, file lock.File) {
	t.Helper()

	content, err := lockfile.Marshal(file)
	if err != nil {
		t.Fatalf("lockfile.Marshal returned error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
}
