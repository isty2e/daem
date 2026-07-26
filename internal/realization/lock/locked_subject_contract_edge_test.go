package lock

import (
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/supply/artifact"
	skillrepair "github.com/isty2e/daem/internal/supply/compat/skill/repair"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestLockedSubjectContractRejectsInvalidFacetMatrix(t *testing.T) {
	exact := mustContractExactIdentity(t, "review")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	path := mustPathContractRealization(t, target.TargetCodex, "AGENTS.md", "managed-path-v1")
	aggregate := mustAggregateContractRealization(t, "review")
	delegated := mustDelegatedContractRealization(t, "review")
	base := LockedSubjectContractInput{
		EntityID:    mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:   mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
		ExactSupply: &exact, Derivation: &direct,
		Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	}

	tests := []struct {
		name   string
		mutate func(*LockedSubjectContractInput)
		want   string
	}{
		{name: "zero entity", mutate: func(input *LockedSubjectContractInput) { input.EntityID = entity.ID{} }, want: "entity id"},
		{name: "zero subject", mutate: func(input *LockedSubjectContractInput) { input.SubjectID = topology.SubjectID{} }, want: "topology id"},
		{name: "zero facets", mutate: func(input *LockedSubjectContractInput) {
			input.ExactSupply = nil
			input.Derivation = nil
		}, want: "requires exact Supply, realization, or both"},
		{name: "resource realization", mutate: func(input *LockedSubjectContractInput) { input.Realization = &path }, want: "resource subject requires exact Supply and no realization"},
		{name: "projection without realization", mutate: func(input *LockedSubjectContractInput) {
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review")
		}, want: "projection subject requires a realization"},
		{name: "path duplicates Supply", mutate: func(input *LockedSubjectContractInput) {
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review")
			input.Realization = &path
		}, want: "must correlate exact Supply by entity"},
		{name: "projection delegated", mutate: func(input *LockedSubjectContractInput) {
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review")
			input.ExactSupply = nil
			input.Derivation = nil
			input.Realization = &delegated
		}, want: "requires managed path or aggregate"},
		{name: "host relation with Supply", mutate: func(input *LockedSubjectContractInput) {
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectHostRelation, "test.relation", "review")
			input.Realization = &delegated
		}, want: "requires delegated realization and no exact Supply"},
		{name: "host relation aggregate", mutate: func(input *LockedSubjectContractInput) {
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectHostRelation, "test.relation", "review")
			input.ExactSupply = nil
			input.Derivation = nil
			input.Realization = &aggregate
		}, want: "requires delegated realization"},
		{name: "unsupported topology kind", mutate: func(input *LockedSubjectContractInput) {
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectBinding, "test.binding", "review")
		}, want: "locked subject kind"},
		{name: "unknown ownership", mutate: func(input *LockedSubjectContractInput) {
			input.Ownership = "future"
		}, want: "ownership basis"},
		{name: "unknown absence policy", mutate: func(input *LockedSubjectContractInput) {
			input.OnAbsent = "future"
		}, want: "on-absent policy"},
		{name: "missing operation contracts", mutate: func(input *LockedSubjectContractInput) {
			input.OperationContracts = nil
		}, want: "requires at least one operation contract"},
		{name: "duplicate operation contracts", mutate: func(input *LockedSubjectContractInput) {
			input.OperationContracts = append(input.OperationContracts, input.OperationContracts[0])
		}, want: "duplicate operation contract"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := NewLockedSubjectContract(input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLockedSubjectContractRejectsInvalidExactFileUseCombinations(t *testing.T) {
	fileExact := mustContractExactIdentityOfKind(t, "guard", artifact.ArtifactKindFile)
	fileDirect, err := NewDirectResolutionDerivation(fileExact)
	if err != nil {
		t.Fatal(err)
	}
	directoryExact := mustContractExactIdentity(t, "guard-directory")
	directoryDirect, err := NewDirectResolutionDerivation(directoryExact)
	if err != nil {
		t.Fatal(err)
	}
	projectUse, err := NewExactFileUse(target.ScopeProject, true)
	if err != nil {
		t.Fatal(err)
	}
	globalUse, err := NewExactFileUse(target.ScopeGlobal, true)
	if err != nil {
		t.Fatal(err)
	}
	path := mustPathContractRealization(t, target.TargetCodex, "AGENTS.md", "managed-path-v1")
	aggregate := mustAggregateContractRealization(t, "guard")

	tests := []struct {
		name  string
		input LockedSubjectContractInput
		want  string
	}{
		{
			name: "missing exact Supply",
			input: LockedSubjectContractInput{
				EntityID:     mustContractEntityID(t, entity.KindMCPServer, "guard"),
				SubjectID:    mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "guard"),
				ExactFileUse: &projectUse,
				Realization:  &aggregate,
				Ownership:    OwnershipManifest, OnAbsent: OnAbsentRemoveBinding,
				Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractProjectionOperations(t, "claude-project-mcp-v1"),
			},
			want: "requires exact file Supply identity",
		},
		{
			name: "directory Supply",
			input: LockedSubjectContractInput{
				EntityID:     mustContractEntityID(t, entity.KindSkill, "guard-directory"),
				SubjectID:    mustTopologySubjectID(t, topology.SubjectResource, "skill", "guard-directory"),
				ExactSupply:  &directoryExact,
				ExactFileUse: &projectUse,
				Derivation:   &directoryDirect,
				Ownership:    OwnershipManifest, OnAbsent: OnAbsentApply,
				Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
			},
			want: "requires exact file Supply identity",
		},
		{
			name: "aggregate realization",
			input: LockedSubjectContractInput{
				EntityID:     mustContractEntityID(t, entity.KindMCPServer, "guard"),
				SubjectID:    mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "guard"),
				ExactSupply:  &fileExact,
				ExactFileUse: &projectUse,
				Realization:  &aggregate,
				Derivation:   &fileDirect,
				Ownership:    OwnershipManifest, OnAbsent: OnAbsentRemoveBinding,
				Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractProjectionOperations(t, "claude-project-mcp-v1"),
			},
			want: "requires managed file path realization when realized",
		},
		{
			name: "path scope mismatch",
			input: LockedSubjectContractInput{
				EntityID:     mustContractEntityID(t, entity.KindInstructions, "guard"),
				SubjectID:    mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "guard"),
				ExactSupply:  &fileExact,
				ExactFileUse: &globalUse,
				Realization:  &path,
				Derivation:   &fileDirect,
				Ownership:    OwnershipManifest, OnAbsent: OnAbsentApply,
				Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractProjectionOperations(t, "managed-path-v1"),
			},
			want: "does not match managed path scope",
		},
		{
			name: "noncanonical scope",
			input: LockedSubjectContractInput{
				EntityID:     mustContractEntityID(t, entity.KindHookAsset, "guard"),
				SubjectID:    mustTopologySubjectID(t, topology.SubjectResource, "hook_asset", "guard"),
				ExactSupply:  &fileExact,
				ExactFileUse: &ExactFileUse{scope: "local", executable: true},
				Derivation:   &fileDirect,
				Ownership:    OwnershipManifest, OnAbsent: OnAbsentApply,
				Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
			},
			want: "unknown scope",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewLockedSubjectContract(test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLockedSubjectContractRejectsResourceSubjectThatDoesNotMatchEntity(t *testing.T) {
	exact := mustContractExactIdentity(t, "review")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	_, err = NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectResource, "skill", "other"),
		ExactSupply:        &exact,
		Derivation:         &direct,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	})
	if err == nil || !strings.Contains(err.Error(), "does not match entity") {
		t.Fatalf("NewLockedSubjectContract error = %v", err)
	}
}

func TestLockedSubjectContractRejectsRepairRecipeCorrelationDrift(t *testing.T) {
	recipe := testSkillRepairRecipe(t)
	output := recipe.Output()
	validDerivation := mustContractRepairDerivation(t, recipe)
	direct, err := NewDirectResolutionDerivation(recipe.Output())
	if err != nil {
		t.Fatal(err)
	}
	staleHashDerivation, err := NewDeterministicTransformDerivation(DeterministicTransform{
		InputIdentity:          recipe.Input(),
		RecipeHash:             "sha256:stale",
		AlgorithmID:            skillrepair.DerivationAlgorithmID,
		AlgorithmVersion:       "v1",
		ExecutionDomain:        skillrepair.DerivationExecutionDomain,
		ExpectedOutputIdentity: recipe.Output(),
	})
	if err != nil {
		t.Fatal(err)
	}
	otherInput := mustSkillIdentity(t, "other-input")
	wrongInputDerivation, err := NewDeterministicTransformDerivation(DeterministicTransform{
		InputIdentity:          otherInput,
		RecipeHash:             recipe.Hash(),
		AlgorithmID:            skillrepair.DerivationAlgorithmID,
		AlgorithmVersion:       "v1",
		ExecutionDomain:        skillrepair.DerivationExecutionDomain,
		ExpectedOutputIdentity: recipe.Output(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wrongAlgorithm := mustContractRepairDerivationWithExecutor(
		t, recipe, "other.algorithm", "v1", skillrepair.DerivationExecutionDomain,
	)
	wrongVersion := mustContractRepairDerivationWithExecutor(
		t, recipe, skillrepair.DerivationAlgorithmID, "v2", skillrepair.DerivationExecutionDomain,
	)
	wrongDomain := mustContractRepairDerivationWithExecutor(
		t, recipe, skillrepair.DerivationAlgorithmID, "v1", "other:domain",
	)

	base := LockedSubjectContractInput{
		EntityID:           mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:          mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
		ExactSupply:        &output,
		Derivation:         &validDerivation,
		RepairRecipe:       &recipe,
		Ownership:          OwnershipManifest,
		OnAbsent:           OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayExact),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	}
	tests := []struct {
		name   string
		mutate func(*LockedSubjectContractInput)
		want   string
	}{
		{name: "missing derivation", mutate: func(input *LockedSubjectContractInput) { input.Derivation = nil }, want: "requires exact Supply and deterministic derivation"},
		{name: "direct derivation", mutate: func(input *LockedSubjectContractInput) { input.Derivation = &direct }, want: "requires deterministic derivation"},
		{name: "recipe hash mismatch", mutate: func(input *LockedSubjectContractInput) { input.Derivation = &staleHashDerivation }, want: "does not match deterministic derivation"},
		{name: "recipe input mismatch", mutate: func(input *LockedSubjectContractInput) { input.Derivation = &wrongInputDerivation }, want: "does not match deterministic derivation"},
		{name: "wrong entity family", mutate: func(input *LockedSubjectContractInput) {
			input.EntityID = mustContractEntityID(t, entity.KindHook, "review")
			input.SubjectID = mustTopologySubjectID(t, topology.SubjectResource, "hook", "review")
		}, want: "requires Skill entity"},
		{name: "wrong algorithm", mutate: func(input *LockedSubjectContractInput) { input.Derivation = &wrongAlgorithm }, want: "does not match skill repair execution contract"},
		{name: "wrong algorithm version", mutate: func(input *LockedSubjectContractInput) { input.Derivation = &wrongVersion }, want: "does not match skill repair execution contract"},
		{name: "wrong execution domain", mutate: func(input *LockedSubjectContractInput) { input.Derivation = &wrongDomain }, want: "does not match skill repair execution contract"},
		{name: "nonexact derivation replay", mutate: func(input *LockedSubjectContractInput) {
			input.Replay = mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayPartial)
		}, want: "requires exact derivation replay coverage"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := base
			test.mutate(&input)
			if _, err := NewLockedSubjectContract(input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLockedSubjectContractRejectsCrossAxisPolicyDrift(t *testing.T) {
	exact := mustContractExactIdentity(t, "review")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	other := mustContractExactIdentity(t, "other")
	mismatchedDerivation, err := NewDirectResolutionDerivation(other)
	if err != nil {
		t.Fatal(err)
	}
	aggregate := mustAggregateContractRealization(t, "review")
	delegated := mustDelegatedContractRealization(t, "review")
	filePath := mustPathContractRealization(t, target.TargetCodex, "AGENTS.md", "managed-path-v1")
	plan := DelegatePlanIdentityFromPlan(mustDelegatePlan(t, delegate.RunnerPlain, "node", []string{"server.js"}, nil, delegate.PinNotApplicable))

	tests := []struct {
		name  string
		input LockedSubjectContractInput
		want  string
	}{
		{
			name: "path duplicates Supply facet",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindSkill, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review"),
				ExactSupply: &exact, Realization: &filePath, Derivation: &direct,
				Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
				Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractProjectionOperations(t, "managed-path-v1"),
			},
			want: "must correlate exact Supply by entity",
		},
		{
			name: "derivation without Supply",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindMCPServer, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review"),
				Realization: &aggregate, Derivation: &direct,
				Ownership: OwnershipManifest, OnAbsent: OnAbsentRemoveBinding,
				Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractProjectionOperations(t, "claude-project-mcp-v1"),
			},
			want: "derivation requires exact Supply",
		},
		{
			name: "derivation output mismatch",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindSkill, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
				ExactSupply: &exact, Derivation: &mismatchedDerivation,
				Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
				Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
			},
			want: "must match exact Supply identity",
		},
		{
			name: "delegate plan without aggregate",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindSkill, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
				ExactSupply: &exact, Derivation: &direct, DelegatePlanIdentity: &plan,
				Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
				Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
			},
			want: "requires managed aggregate realization",
		},
		{
			name: "delegated replay claims exact outcome",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindExtension, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectHostRelation, "test.relation", "review"),
				Realization: &delegated,
				Ownership:   OwnershipManifest, OnAbsent: OnAbsentBlock,
				Replay:             mustContractReplay(t, ReplayPartial, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractDelegatedOperations(t, "codex.plugin.install", "codex-plugin-v1"),
			},
			want: "must not be outcome replayable",
		},
		{
			name: "delegated route mismatch",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindExtension, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectHostRelation, "test.relation", "review"),
				Realization: &delegated,
				Ownership:   OwnershipManifest, OnAbsent: OnAbsentBlock,
				Replay:             mustContractReplay(t, ReplayPartial, ReplayUnavailable, ReplayNotApplicable),
				OperationContracts: contractDelegatedOperations(t, "other.route", "codex-plugin-v1"),
			},
			want: "does not match realization route",
		},
		{
			name: "aggregate codec mismatch",
			input: LockedSubjectContractInput{
				EntityID:    mustContractEntityID(t, entity.KindMCPServer, "review"),
				SubjectID:   mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review"),
				Realization: &aggregate,
				Ownership:   OwnershipManifest, OnAbsent: OnAbsentRemoveBinding,
				Replay:             mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
				OperationContracts: contractProjectionOperations(t, "other-adapter-v1"),
			},
			want: "does not match managed aggregate codec contract",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewLockedSubjectContract(test.input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestLockedSubjectContractRejectsMisplacedSkillSetCorrelation(t *testing.T) {
	exact := mustContractExactIdentity(t, "review")
	direct, err := NewDirectResolutionDerivation(exact)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSkillSetMemberCorrelation(desiredskill.SkillSetDeclarationIdentity{}); err == nil {
		t.Fatal("zero SkillSet declaration identity was accepted")
	}

	set := testSkillSet(t, sourcetest.Local(t, "skills", source.LocalSourceModeVendor), []string{"glob:*"})
	declarationIdentity, err := set.DeclarationIdentity()
	if err != nil {
		t.Fatal(err)
	}
	correlation, err := NewSkillSetMemberCorrelation(declarationIdentity)
	if err != nil {
		t.Fatal(err)
	}
	base := LockedSubjectContractInput{
		EntityID:    mustContractEntityID(t, entity.KindSkill, "review"),
		SubjectID:   mustTopologySubjectID(t, topology.SubjectResource, "skill", "review"),
		ExactSupply: &exact, Derivation: &direct, SkillSetMemberCorrelation: &correlation,
		Ownership: OwnershipManifest, OnAbsent: OnAbsentApply,
		Replay:             mustContractReplay(t, ReplayUnavailable, ReplayExact, ReplayNotApplicable),
		OperationContracts: contractSupplyOperations(t, "snapshot.resource.v1"),
	}

	wrongEntity := base
	wrongEntity.EntityID = mustContractEntityID(t, entity.KindInstructions, "review")
	wrongEntity.SubjectID = mustTopologySubjectID(t, topology.SubjectResource, "instructions", "review")
	if _, err := NewLockedSubjectContract(wrongEntity); err == nil || !strings.Contains(err.Error(), "requires Skill entity") {
		t.Fatalf("wrong entity error = %v", err)
	}

	aggregate := mustAggregateContractRealization(t, "review")
	wrongSubject := base
	wrongSubject.SubjectID = mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review")
	wrongSubject.ExactSupply = nil
	wrongSubject.Derivation = nil
	wrongSubject.Realization = &aggregate
	if _, err := NewLockedSubjectContract(wrongSubject); err == nil || !strings.Contains(err.Error(), "requires exact-Supply artifact subject") {
		t.Fatalf("wrong subject error = %v", err)
	}
}

func TestLockedSubjectContractValidatesOperationCompatibilityInCanonicalOrder(t *testing.T) {
	realization := mustPathContractRealization(t, target.TargetCodex, "AGENTS.md", "managed-path-v1")
	remove := mustOperationContract(t, OperationContractInput{
		Operation: OperationRemove, Actuation: ActuationDelegatedHostRoute, Authority: AuthorityRemove,
		Route:          RouteContractRef{RouteID: "test.remove", AdapterContractVersion: "managed-path-v1"},
		EffectEnvelope: EffectEnvelopeIncomplete, Idempotency: IdempotencyUnknown,
		Verification: VerificationInsufficient, TrustActivation: TrustActivationUnknown,
		Recovery: OperationRecoveryUnknown,
	})
	install := mustOperationContract(t, OperationContractInput{
		Operation: OperationInstall, Actuation: ActuationDelegatedHostRoute, Authority: AuthorityManage,
		Route:          RouteContractRef{RouteID: "test.install", AdapterContractVersion: "managed-path-v1"},
		EffectEnvelope: EffectEnvelopeIncomplete, Idempotency: IdempotencyUnknown,
		Verification: VerificationInsufficient, TrustActivation: TrustActivationUnknown,
		Recovery: OperationRecoveryUnknown,
	})

	_, err := NewLockedSubjectContract(LockedSubjectContractInput{
		EntityID:    mustContractEntityID(t, entity.KindInstructions, "review"),
		SubjectID:   mustTopologySubjectID(t, topology.SubjectProjection, "test.projection", "review"),
		Realization: &realization,
		Ownership:   OwnershipManifest,
		OnAbsent:    OnAbsentApply,
		Replay:      mustContractReplay(t, ReplayExact, ReplayExact, ReplayNotApplicable),
		// Reverse lexical input order to prove validation is owned by canonical keys.
		OperationContracts: []OperationContract{remove, install},
	})
	if err == nil || !strings.Contains(err.Error(), `managed projection operation "install" must not use delegated host route`) {
		t.Fatalf("NewLockedSubjectContract error = %v, want canonical install-first validation", err)
	}
}
