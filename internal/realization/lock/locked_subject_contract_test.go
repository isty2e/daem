package lock

import (
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestLockedSubjectContractAdmitsOrthogonalFacetShapes(t *testing.T) {
	exact := mustContractExactIdentity(t, "review")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}

	supplyOnly := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
		ExactSupply:        &exact,
		Derivation:         &direct,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	})
	if got, ok := supplyOnly.ExactSupply(); !ok || !got.Equal(exact) {
		t.Fatalf("ExactSupply = %#v, %t", got, ok)
	}
	if _, ok := supplyOnly.Realization(); ok {
		t.Fatal("Supply-only contract exposed realization")
	}

	pathRealization := mustPathContractRealization(t, target.TargetCodex, "AGENTS.md", "managed-path-v1")
	path := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindInstructions, "project"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectProjection, "codex.project.instructions", "project"),
		Realization:        &pathRealization,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractProjectionOperations(t, "managed-path-v1"),
	})
	if got, ok := path.Realization(); !ok || !got.Equal(pathRealization) {
		t.Fatalf("path Realization = %#v, %t", got, ok)
	}

	aggregateRealization := mustAggregateContractRealization(t, "context7")
	aggregate := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindMCPServer, "context7"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectProjection, "claude-code.project.mcp-server", "context7"),
		Realization:        &aggregateRealization,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentRemoveBinding,
		Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractProjectionOperations(t, "claude-project-mcp-v1"),
	})
	if _, ok := aggregate.ExactSupply(); ok {
		t.Fatal("aggregate-only contract exposed exact Supply")
	}

	delegatedRealization := mustDelegatedContractRealization(t, "review")
	delegated := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindExtension, "review"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectHostRelation, "codex.plugin-carrier", "review"),
		Realization:        &delegatedRealization,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentBlock,
		Replay:             mustContractReplay(t, ReplayPartial, ReplayUnavailable, ReplayNotApplicable),
		OperationContracts: contractDelegatedOperations(t, "codex.plugin.install", "codex-plugin-v1"),
	})
	if got, ok := delegated.Realization(); !ok || !got.Equal(delegatedRealization) {
		t.Fatalf("delegated Realization = %#v, %t", got, ok)
	}
}

func TestLockedSubjectContractIdentityAndEqualityPreserveCardinality(t *testing.T) {
	entityID := mustContractEntityID(t, entity.KindInstructions, "shared")
	firstRealization := mustPathContractRealization(t, target.TargetCodex, "AGENTS.md", "managed-path-v1")
	secondRealization := mustPathContractRealization(t, target.TargetClaudeCode, "CLAUDE.md", "managed-path-v1")

	firstInput := LockedSubjectContractInput{
		EntityID: entityID, SubjectID: mustTopologySubjectID(t, topology.SubjectProjection, "codex.project.instructions", "shared"),
		Realization: &firstRealization,
		Ownership:   OwnershipManifest, OnAbsent: OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractProjectionOperations(t, "managed-path-v1"),
	}
	secondInput := firstInput
	secondInput.SubjectID = mustTopologySubjectID(t, topology.SubjectProjection, "claude-code.project.instructions", "shared")
	secondInput.Realization = &secondRealization

	first := mustLockedSubjectContract(t, firstInput)
	identical := mustLockedSubjectContract(t, firstInput)
	second := mustLockedSubjectContract(t, secondInput)
	if !first.Equal(identical) || !identical.Equal(first) {
		t.Fatal("identical contracts were not symmetrically equal")
	}
	if first.Equal(second) {
		t.Fatal("different lowered subjects compared equal")
	}
	if first.EntityID() != second.EntityID() {
		t.Fatal("same Desired entity did not survive multiple subjects")
	}
	if _, ok := first.ExactSupply(); ok {
		t.Fatal("projection duplicated exact Supply identity")
	}
	if first.CompareIdentity(second) == 0 || first.CompareIdentity(second) != -second.CompareIdentity(first) {
		t.Fatal("identity comparison did not order distinct subjects antisymmetrically")
	}

	driftedInput := firstInput
	driftedInput.Replay = mustContractReplay(t, ReplayPartial, ReplayExact, ReplayNotApplicable)
	drifted := mustLockedSubjectContract(t, driftedInput)
	if first.Equal(drifted) {
		t.Fatal("replay drift compared equal")
	}
}

func TestLockedSubjectContractCarriesContextualSkillSetMembership(t *testing.T) {
	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	declarationIdentity, err := set.DeclarationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := NewSkillSetMemberCorrelation(declarationIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.TypeFor[SkillSetMemberCorrelation]().NumField() != 1 {
		t.Fatal("SkillSet member correlation duplicated contextual child identity")
	}

	exact := mustContractExactIdentity(t, "selected")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	contract := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:                  mustContractEntityID(t, entity.KindSkill, "selected"),
		SubjectID:                 mustTopologySubjectID(t, topology.SubjectResource, "skill", "selected"),
		ExactSupply:               &exact,
		Derivation:                &direct,
		SkillSetMemberCorrelation: &correlation,
		Ownership:                 OwnershipManifest,
		OnAbsent:                  OnAbsentApply,
		Replay:                    mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts:        contractSupplyOperations(t, "snapshot.resource.v1"),
	})
	got, ok := contract.SkillSetMemberCorrelation()
	if !ok || !got.Equal(correlation) || !got.DeclarationIdentity().Equal(declarationIdentity) {
		t.Fatalf("SkillSetMemberCorrelation = %#v, %t", got, ok)
	}
}

func TestLockedSubjectContractCarriesExactFileUseWithoutConflatingSupplyIdentity(t *testing.T) {
	exact := mustContractExactIdentityOfKind(t, "guard", artifact.ArtifactKindFile)
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	fileUse, err := NewExactFileUse(target.ScopeProject, true)
	if err != nil {
		t.Fatal(err)
	}

	contract := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindHookAsset, "guard"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectResource, "hook_asset", "guard"),
		ExactSupply:        &exact,
		ExactFileUse:       &fileUse,
		Derivation:         &direct,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	})

	lockedSupply, ok := contract.ExactSupply()
	if !ok || !lockedSupply.Equal(exact) {
		t.Fatalf("ExactSupply = %#v, %t", lockedSupply, ok)
	}
	lockedUse, ok := contract.ExactFileUse()
	if !ok || !lockedUse.Equal(fileUse) || lockedUse.Scope() != target.ScopeProject || !lockedUse.Executable() {
		t.Fatalf("ExactFileUse = %#v, %t", lockedUse, ok)
	}

	differentUse, err := NewExactFileUse(target.ScopeProject, false)
	if err != nil {
		t.Fatal(err)
	}
	drifted := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           contract.EntityID(),
		SubjectID:          contract.SubjectID(),
		ExactSupply:        &exact,
		ExactFileUse:       &differentUse,
		Derivation:         &direct,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	})
	if contract.Equal(drifted) {
		t.Fatal("executable intent drift compared equal")
	}
}

func TestLockedSubjectContractProjectsMaterializedFileIdentityWithoutReplacingSupply(t *testing.T) {
	content := []byte("#!/bin/sh\nexit 0\n")
	raw := mustContractExactFileIdentity(t, "guard", content, false)
	materialization, err := artifact.NewFileMaterialization(raw, content, false, true)
	if err != nil {
		t.Fatal(err)
	}
	derivation, err := NewFileMaterializationDerivation(materialization)
	if err != nil {
		t.Fatal(err)
	}
	use, err := NewExactFileUse(target.ScopeProject, true)
	if err != nil {
		t.Fatal(err)
	}
	contract := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:    mustContractEntityID(t, entity.KindHookAsset, "guard"),
		SubjectID:   mustResourceSubjectID(t, mustContractEntityID(t, entity.KindHookAsset, "guard")),
		ExactSupply: &raw, ExactFileUse: &use, Derivation: &derivation,
		Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayExact),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	})

	got, ok := contract.MaterializedFileIdentity()
	if !ok || !got.Equal(materialization.OutputIdentity()) {
		t.Fatalf("MaterializedFileIdentity = %#v, %t; want %#v", got, ok, materialization.OutputIdentity())
	}
	supply, _ := contract.ExactSupply()
	if !supply.Equal(raw) {
		t.Fatalf("ExactSupply = %#v, want raw %#v", supply, raw)
	}
}

func TestExactSupplySubjectMarksEveryDeterministicDerivationExactlyReplayable(t *testing.T) {
	content := []byte("#!/bin/sh\nexit 0\n")
	seed := mustContractExactIdentityOfKind(t, "hook-input", artifact.ArtifactKindFile)
	input, err := artifact.NewExactIdentity(
		seed.SourceID(),
		seed.ResolvedRef(),
		artifact.ArtifactKindFile,
		artifact.HashFileContentWithExecutable(content, false),
	)
	if err != nil {
		t.Fatal(err)
	}
	materialization, err := artifact.NewFileMaterialization(input, content, false, true)
	if err != nil {
		t.Fatal(err)
	}
	derivation, err := NewFileMaterializationDerivation(materialization)
	if err != nil {
		t.Fatal(err)
	}
	use, err := NewExactFileUse(target.ScopeProject, true)
	if err != nil {
		t.Fatal(err)
	}
	entityID := mustContractEntityID(t, entity.KindHookAsset, "guard")
	contract, err := NewExactSupplySubjectContract(ExactSupplySubjectInput{
		EntityID:     entityID,
		SubjectID:    mustResourceSubjectID(t, entityID),
		ExactSupply:  materialization.InputIdentity(),
		ExactFileUse: &use,
		Derivation:   derivation,
	})
	if err != nil {
		t.Fatal(err)
	}
	if contract.ReplayCoverage().Derivation() != ReplayExact {
		t.Fatalf("derivation replay = %q, want exact", contract.ReplayCoverage().Derivation())
	}
}

func TestLockedSubjectContractCarriesCanonicalRepairRecipe(t *testing.T) {
	recipe := testSkillRepairRecipe(t)
	output := recipe.Output()
	derivation := mustContractRepairDerivation(t, recipe)
	contract := mustLockedSubjectContract(t, LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
		ExactSupply:        &output,
		Derivation:         &derivation,
		RepairRecipe:       &recipe,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayExact),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	})

	inverse, err := recipe.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	recipe = inverse

	lockedRecipe, ok := contract.RepairRecipe()
	if !ok || !lockedRecipe.Equal(testSkillRepairRecipe(t)) {
		t.Fatalf("RepairRecipe = %#v, %t", lockedRecipe, ok)
	}
	if lockedRecipe.Equal(recipe) {
		t.Fatal("contract recipe changed through caller-owned recipe variable")
	}
	if !contract.Equal(contract) {
		t.Fatal("valid repair contract is not reflexively equal")
	}
}

func mustContractRepairDerivation(t *testing.T, recipe skillrepair.Recipe) DerivationContract {
	t.Helper()
	return mustContractRepairDerivationWithExecutor(
		t,
		recipe,
		skillrepair.DerivationAlgorithmID,
		"v1",
		skillrepair.DerivationExecutionDomain,
	)
}

func mustContractRepairDerivationWithExecutor(
	t *testing.T,
	recipe skillrepair.Recipe,
	algorithmID string,
	algorithmVersion string,
	executionDomain string,
) DerivationContract {
	t.Helper()
	derivation, err := NewDeterministicTransformDerivation(DeterministicTransform{
		InputIdentity:          recipe.Input(),
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            algorithmID,
		AlgorithmVersion:       algorithmVersion,
		ExecutionDomain:        executionDomain,
		ExpectedOutputIdentity: recipe.Output(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return derivation
}

func mustContractEntityID(t *testing.T, kind entity.Kind, name string) entity.ID {
	t.Helper()
	id, err := entity.New(kind, name)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func mustContractExactIdentity(t *testing.T, name string) artifact.ExactIdentity {
	t.Helper()
	return mustContractExactIdentityOfKind(t, name, artifact.ArtifactKindDirectory)
}

func mustContractExactIdentityOfKind(
	t *testing.T,
	name string,
	kind artifact.ArtifactKind,
) artifact.ExactIdentity {
	t.Helper()
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("local:skills/"+name),
		artifact.ResolvedRef(""),
		kind,
		testExactHash(name),
	)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func mustContractExactFileIdentity(
	t *testing.T,
	name string,
	content []byte,
	executable bool,
) artifact.ExactIdentity {
	t.Helper()
	identity, err := artifact.NewExactIdentity(
		artifact.SourceID("local:hooks/"+name),
		artifact.ResolvedRef(""),
		artifact.ArtifactKindFile,
		artifact.HashFileContentWithExecutable(content, executable),
	)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func mustLockedSubjectContract(t *testing.T, input LockedSubjectContractInput) LockedSubjectContract {
	t.Helper()
	contract, err := NewLockedSubjectContract(input)
	if err != nil {
		t.Fatalf("NewLockedSubjectContract returned error: %v", err)
	}
	return contract
}

func mustContractReplay(t *testing.T, invocation ReplayClass, outcome ReplayClass, derivation ReplayClass) ReplayCoverage {
	t.Helper()
	coverage, err := NewReplayCoverage(invocation, outcome, derivation, nil)
	if err != nil {
		t.Fatal(err)
	}
	return coverage
}

func mustPathContractRealization(t *testing.T, selectedTarget target.Target, destination string, adapter string) realization.RealizationSpec {
	t.Helper()
	realization, err := realization.NewManagedPathProjection(realization.ManagedPathProjectionInput{
		PlacementID: "instructions.project", ConsumerTargets: []target.Target{selectedTarget}, Scope: target.ScopeProject,
		Destination: destination, ContentKind: realization.PathProjectionFile, PlacementMode: realization.PathProjectionCopy,
		PermissionPolicy:       realization.PathPermissionsExecutableClass,
		AdapterContractVersion: adapter,
	})
	if err != nil {
		t.Fatal(err)
	}
	return realization
}

func mustAggregateContractRealization(t *testing.T, name string) realization.RealizationSpec {
	t.Helper()
	realization, err := realization.NewManagedAggregateContribution(aggregate.ManagedContributionInput{
		PlacementID: "claude-code.project.mcp", Target: target.TargetClaudeCode, Scope: target.ScopeProject,
		AggregateRoot: ".mcp.json", ContentPath: "/mcpServers/" + name,
		MergeUnit:             aggregate.MergeUnit("mcp-server-entry"),
		Cardinality:           aggregate.ContributionExclusive,
		SiblingRetention:      aggregate.PreserveUnmanagedSiblings,
		SiblingPreservation:   aggregate.PreserveSiblingsSemantic,
		Equivalence:           aggregate.EquivalenceCanonicalSemantic,
		CanonicalContribution: `{"command":"npx"}`,
		CodecContractID:       "claude-project-mcp-v1", ComparedFields: []string{"command", "type"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return realization
}

func mustDelegatedContractRealization(t *testing.T, name string) realization.RealizationSpec {
	t.Helper()
	subjectKey, err := hostrelation.NewSubjectKey(name)
	if err != nil {
		t.Fatal(err)
	}
	managedKey, err := hostrelation.NewManagedInstanceKey("managed:" + name)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.NewExpectedRelation(subjectKey, managedKey)
	if err != nil {
		t.Fatal(err)
	}
	realization, err := realization.NewDelegatedRelation(realization.DelegatedRelationInput{
		PlacementID: "codex-plugin", Target: target.TargetCodex, Scope: target.ScopeGlobal,
		SourceNamespace: "marketplace:" + name, ExpectedRelation: expected, RouteID: "codex.plugin.install",
		RouteContractVersion: "codex-plugin-v1", CanonicalRequestHash: string(testExactHash("request-" + name)),
		VerifiedRelationFields: []string{"scope", "source_ref", "target"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return realization
}

func contractSupplyOperations(t *testing.T, adapter string) []OperationContract {
	t.Helper()
	return []OperationContract{
		mustOperationContract(t, OperationContractInput{
			Operation: OperationWriteProjection, Actuation: ActuationDirectProjection, Authority: AuthorityManage,
			Route:          RouteContractRef{RouteID: "snapshot.resource.write", AdapterContractVersion: adapter},
			EffectEnvelope: EffectEnvelopeComplete, Idempotency: Idempotent,
			Verification: VerificationExactArtifact, TrustActivation: TrustActivationNotRequired, Recovery: OperationRecoveryAtomic,
		}),
		contractObserveOperation(t, VerificationExactArtifact),
	}
}

func contractProjectionOperations(t *testing.T, adapter string) []OperationContract {
	t.Helper()
	return []OperationContract{
		mustOperationContract(t, OperationContractInput{
			Operation: OperationWriteProjection, Actuation: ActuationDirectProjection, Authority: AuthorityManage,
			Route:          RouteContractRef{RouteID: "projection.write", AdapterContractVersion: adapter},
			EffectEnvelope: EffectEnvelopeComplete, Idempotency: Idempotent,
			Verification: VerificationExactProjection, TrustActivation: TrustActivationNotRequired, Recovery: OperationRecoveryAtomic,
		}),
		contractObserveOperation(t, VerificationExactProjection),
	}
}

func contractDelegatedOperations(t *testing.T, route string, adapter string) []OperationContract {
	t.Helper()
	return []OperationContract{mustOperationContract(t, OperationContractInput{
		Operation: OperationInstall, Actuation: ActuationDelegatedHostRoute, Authority: AuthorityManage,
		Route:          RouteContractRef{RouteID: route, AdapterContractVersion: adapter},
		EffectEnvelope: EffectEnvelopeIncomplete, Idempotency: IdempotencyUnknown,
		Verification: VerificationInsufficient, TrustActivation: TrustActivationUnknown, Recovery: OperationRecoveryUnknown,
	})}
}

func contractObserveOperation(t *testing.T, verification VerificationContract) OperationContract {
	t.Helper()
	return mustOperationContract(t, OperationContractInput{
		Operation: OperationObserve, Actuation: ActuationNoMutation, Authority: AuthorityObserve,
		EffectEnvelope: EffectEnvelopeNotApplicable, Idempotency: IdempotencyNotApplicable,
		Verification: verification, TrustActivation: TrustActivationNotRequired, Recovery: OperationRecoveryNotApplicable,
	})
}
