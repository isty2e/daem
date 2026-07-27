package apply

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	relationobserve "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/skill"
	desiredtest "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/output/hostpath"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
	"github.com/isty2e/daem/test/outputtest"
)

func TestBuildApplyAuthorityEvidenceCoversAuthoritativePaths(t *testing.T) {
	planned := applyAuthorityTestPlan(t)
	rootAuthority, err := planned.projectRoot.Authority()
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := buildApplyAuthorityEvidence(t.Context(), planned)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.domains) != 13 {
		t.Fatalf("domain count = %d, want 13", len(evidence.domains))
	}
	if len(evidence.revisions) != 13 {
		t.Fatalf("revision count = %d, want 13: %#v", len(evidence.revisions), evidence.revisions)
	}
	metadataTransactionPath, err := transaction.FileSetAuthorityPath(planned.context.Paths.StateDir)
	if err != nil {
		t.Fatal(err)
	}

	want := map[mutation.RevisionRequest]bool{
		{Path: planned.result.ManifestPath, Effect: mutation.PathEffectDirectoryEntry}:                                                false,
		{Path: planned.result.ManifestPath, Effect: mutation.PathEffectReferent}:                                                      false,
		{Path: planned.result.LockfilePath, Effect: mutation.PathEffectDirectoryEntry}:                                                false,
		{Path: planned.result.LockfilePath, Effect: mutation.PathEffectReferent}:                                                      false,
		{Path: planned.result.StatefilePath, Effect: mutation.PathEffectDirectoryEntry}:                                               false,
		{Path: planned.result.StatefilePath, Effect: mutation.PathEffectReferent}:                                                     false,
		{Path: planned.context.Paths.RecoveryDir, Effect: mutation.PathEffectDirectoryEntry}:                                          false,
		{Path: planned.context.Paths.RecoveryDir, Effect: mutation.PathEffectReferent}:                                                false,
		{Path: metadataTransactionPath, Effect: mutation.PathEffectDirectoryEntry}:                                                    false,
		{Path: filepath.Join(planned.context.Paths.ManifestRoot, "skills", "review"), Effect: mutation.PathEffectDirectoryEntry}:      false,
		{Path: filepath.Join(planned.context.Paths.ManifestRoot, "skills", "review"), Effect: mutation.PathEffectReferent}:            false,
		{Path: filepath.Join(rootAuthority.PhysicalRoot(), ".claude", "skills", "review"), Effect: mutation.PathEffectDirectoryEntry}: false,
		{Path: filepath.Join(rootAuthority.PhysicalRoot(), ".claude", "skills", "review"), Effect: mutation.PathEffectReferent}:       false,
	}
	for _, request := range evidence.revisions {
		if _, ok := want[request]; !ok {
			t.Fatalf("unexpected revision request %#v", request)
		}
		want[request] = true
	}
	for request, found := range want {
		if !found {
			t.Fatalf("missing revision request %#v", request)
		}
	}

	repeated, err := buildApplyAuthorityEvidence(t.Context(), planned)
	if err != nil {
		t.Fatal(err)
	}
	if !evidence.authorityFingerprint.Equal(repeated.authorityFingerprint) {
		t.Fatal("same authority facts produced different fingerprints")
	}

	changed := planned
	changed.assessment.Reconciliation = applyAuthorityManagedPathPlan(
		t,
		"review",
		"other",
		"desired",
		target.TargetClaudeCode,
		target.ScopeProject,
		ownership.OwnerAuthority{},
		nil,
	)
	changedEvidence, err := buildApplyAuthorityEvidence(t.Context(), changed)
	if err != nil {
		t.Fatal(err)
	}
	if evidence.authorityFingerprint.Equal(changedEvidence.authorityFingerprint) {
		t.Fatal("authority fingerprint ignored destination change")
	}
}

func TestApplyOperationFingerprintBindsPlanAndDelegateMode(t *testing.T) {
	planned := applyAuthorityTestPlan(t)
	base, err := applyOperationFingerprint(planned, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := applyOperationFingerprint(planned, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}
	if !base.Equal(repeated) {
		t.Fatal("same apply operation produced different fingerprints")
	}
	dryRun, err := applyOperationFingerprint(planned, reconcile.ContextDryRun)
	if err != nil {
		t.Fatal(err)
	}
	if base.Equal(dryRun) {
		t.Fatal("fingerprint ignored delegate policy mode")
	}

	disclosureChanged := planned
	disclosureChanged.result.Reconciliation = reconcile.Result{}
	disclosureFingerprint, err := applyOperationFingerprint(disclosureChanged, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}
	if !base.Equal(disclosureFingerprint) {
		t.Fatal("fingerprint depended on disclosure plan instead of canonical assessment")
	}

	changed := planned
	changed.assessment.Reconciliation = applyAuthorityManagedPathPlan(
		t,
		"review",
		"review",
		"changed",
		target.TargetClaudeCode,
		target.ScopeProject,
		ownership.OwnerAuthority{},
		nil,
	)
	changedFingerprint, err := applyOperationFingerprint(changed, reconcile.ContextApply)
	if err != nil {
		t.Fatal(err)
	}
	if base.Equal(changedFingerprint) {
		t.Fatal("fingerprint ignored executable plan change")
	}
}

func TestAggregateFingerprintRowsExcludeUnmanagedDocumentValues(t *testing.T) {
	root := t.TempDir()
	paths := applyTestPaths(t, root)
	selection := applyMCPSelection(t)
	configPath := filepath.Join(root, ".mcp.json")
	writeConfig := func(secret string) {
		t.Helper()
		writeApplyFile(t, configPath, `{
  "mcpServers": {
    "unmanaged": {
      "type": "stdio",
      "command": "keep-me",
      "env": {
        "TOKEN": "`+secret+`"
      }
    }
  }
}
`)
	}
	planRows := func(command string) []aggregateFingerprintFacts {
		t.Helper()
		environment := applyMCPEnvironment(
			t,
			"managed",
			target.TargetClaudeCode,
			command,
			nil,
			nil,
		)
		locked, _ := applyMCPLockfile(t, "managed", command, nil)
		assessment := buildAggregateApplyAssessment(
			t,
			paths,
			environment,
			locked,
			selection,
			false,
		)
		return aggregateFingerprintRows(assessment.Reconciliation.Aggregates())
	}

	writeConfig("SECRET_ALPHA")
	first := planRows("managed-v1")
	writeConfig("SECRET_BETA")
	second := planRows("managed-v1")
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("unmanaged document value changed semantic fingerprint rows:\nfirst = %#v\nsecond = %#v", first, second)
	}

	changedDesired := planRows("managed-v2")
	if reflect.DeepEqual(second, changedDesired) {
		t.Fatal("selected desired contribution change did not change semantic fingerprint rows")
	}
}

func TestProjectAuthorityPathCoalescesSymlinkAliasToPhysicalRoot(t *testing.T) {
	parent := t.TempDir()
	physical := filepath.Join(parent, "physical-project")
	if err := os.Mkdir(physical, 0o700); err != nil {
		t.Fatalf("create physical project root: %v", err)
	}
	alias := filepath.Join(parent, "project-alias")
	if err := os.Symlink(physical, alias); err != nil {
		t.Fatalf("create project-root alias: %v", err)
	}
	planned := commandPlan{
		result: CommandResult{
			Reconciliation: applyAuthorityManagedPathPlan(
				t,
				"review",
				"review",
				"desired",
				target.TargetCodex,
				target.ScopeProject,
				ownership.OwnerAuthority{},
				nil,
			),
		},
		assessment: readiness.Assessment{},
		context:    commandContext{Paths: daempaths.Paths{ManifestRoot: alias}},
	}
	planned.assessment.Reconciliation = planned.result.Reconciliation.Clone()
	captureApplyProjectRootForTest(t, &planned)
	defer closeCommandPlan(&planned)
	decision := planned.result.Reconciliation.ManagedPaths()[0]
	path, err := projectDestinationAuthorityPathFor(planned, decision.Scope(), decision.Destination())
	if err != nil {
		t.Fatalf("projectDestinationAuthorityPath returned error: %v", err)
	}
	wantDestination := filepath.Join(".agents", "skills", "review")
	want, err := filepath.EvalSymlinks(filepath.Join(physical, wantDestination))
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("resolve expected physical destination: %v", err)
	}
	if os.IsNotExist(err) {
		resolvedRoot, resolveErr := filepath.EvalSymlinks(physical)
		if resolveErr != nil {
			t.Fatalf("resolve physical root: %v", resolveErr)
		}
		want = filepath.Join(resolvedRoot, wantDestination)
	}
	if path != want {
		t.Fatalf("authority path = %q, want physical alias-coalesced path %q", path, want)
	}
}

func TestProjectAuthorityPathRejectsInvalidScopeInsteadOfUsingLexicalFallback(t *testing.T) {
	planned := applyAuthorityTestPlan(t)

	if _, err := projectDestinationAuthorityPathFor(planned, "", outputtest.Parse(t, ".claude/skills/review")); err == nil {
		t.Fatal("projectDestinationAuthorityPathFor accepted an invalid scope")
	}
}

func TestBuildApplyAuthorityEvidenceRejectsDistinctLogicalDestinationsAtSamePhysicalPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	paths := daempaths.Paths{
		ManifestRoot:          root,
		RecoveryDir:           filepath.Join(root, ".daem", "recovery"),
		DataDir:               filepath.Join(root, ".daem", "data"),
		OwnershipRegistryPath: filepath.Join(root, ".daem", "data", "ownership", "claims.json"),
		ManifestPath:          filepath.Join(root, "daem.toml"),
		LockfilePath:          filepath.Join(root, "daem.lock.toml"),
		StateDir:              filepath.Join(root, ".daem"),
		StatefilePath:         filepath.Join(root, ".daem", "state.json"),
	}
	owner, err := ownership.NewOwnerAuthority(paths.StatefilePath, paths.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	globalDestination := outputtest.Parse(t, "~/.agents/skills/review")
	globalPath, err := hostpath.NewResolverWithManagedDataRoot(root, paths.DataDir).Resolve(globalDestination)
	if err != nil {
		t.Fatal(err)
	}
	globalAddress, err := ownership.NewManagedAddress(globalPath, "")
	if err != nil {
		t.Fatal(err)
	}
	globalOwnership := []observe.OwnershipObservation{{
		Destination: globalDestination,
		Address:     globalAddress,
		Claim:       ownership.NoClaim(),
	}}
	projectPlan := applyAuthorityManagedPathPlan(
		t,
		"project-review",
		"review",
		"project",
		target.TargetCodex,
		target.ScopeProject,
		ownership.OwnerAuthority{},
		nil,
	)
	globalPlan := applyAuthorityManagedPathPlan(
		t,
		"global-review",
		"review",
		"global",
		target.TargetCodex,
		target.ScopeGlobal,
		owner,
		globalOwnership,
	)
	combinedPlan := mustReconciliationPlan(
		t,
		append(projectPlan.ManagedPaths(), globalPlan.ManagedPaths()...),
		nil,
	)
	planned := commandPlan{
		result: CommandResult{
			ManifestPath:   filepath.Join(root, "daem.toml"),
			LockfilePath:   filepath.Join(root, "daem.lock.toml"),
			StatefilePath:  filepath.Join(root, ".daem", "state.json"),
			Reconciliation: combinedPlan.Clone(),
		},
		assessment: readiness.Assessment{
			StatePath:      filepath.Join(root, ".daem", "state.json"),
			Reconciliation: combinedPlan,
			Owner:          owner,
			Ownership:      globalOwnership,
		},
		context: commandContext{Paths: paths},
	}
	captureApplyProjectRootForTest(t, &planned)
	t.Cleanup(func() { _ = closeCommandPlan(&planned) })

	_, err = buildApplyAuthorityEvidence(t.Context(), planned)
	if err == nil || !strings.Contains(err.Error(), "aliases incompatible logical occupancies") {
		t.Fatalf("buildApplyAuthorityEvidence error = %v, want physical occupancy rejection", err)
	}
}

func TestPhysicalOccupancyIndexAllowsSharedConsumersOnlyAtSameLogicalAddressAndKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	index := make(physicalOccupancyIndex)
	whole := physicalOccupancy{
		scope: target.ScopeProject, destination: outputtest.Parse(t, ".agent/config"), kind: physicalOccupancyWholePath,
	}
	if err := index.register(path, whole); err != nil {
		t.Fatal(err)
	}
	if err := index.register(path, whole); err != nil {
		t.Fatalf("same occupancy was rejected: %v", err)
	}
	aggregate := whole
	aggregate.kind = physicalOccupancyAggregate
	aggregate.target = target.TargetClaudeCode
	if err := index.register(path, aggregate); err == nil {
		t.Fatal("whole-path and aggregate occupancies were admitted at the same physical path")
	}

	index = make(physicalOccupancyIndex)
	if err := index.register(path, aggregate); err != nil {
		t.Fatal(err)
	}
	otherTarget := aggregate
	otherTarget.target = target.TargetCodex
	if err := index.register(path, otherTarget); err == nil {
		t.Fatal("different aggregate targets were admitted at the same physical path")
	}
}

func TestBuildApplyAuthorityEvidenceRevisesRelationObservationPaths(t *testing.T) {
	root := t.TempDir()
	selection, err := targetselection.ForDiagnostics([]string{string(target.TargetCodex)})
	if err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{
		ManifestPath:  filepath.Join(root, "daem.toml"),
		ManifestRoot:  root,
		LockfilePath:  filepath.Join(root, "daem.lock.toml"),
		StateDir:      filepath.Join(root, ".daem"),
		StatefilePath: filepath.Join(root, ".daem", "state.json"),
		RecoveryDir:   filepath.Join(root, ".daem", "recovery"),
		DataDir:       filepath.Join(root, "data"),
	}
	inventoryPath := filepath.Join(root, "host-inventory.json")
	authorityPath, err := relationobserve.NewAuthorityPath(inventoryPath, target.TargetCodex, target.ScopeGlobal)
	if err != nil {
		t.Fatal(err)
	}
	observations, err := relationobserve.NewBatch(relationobserve.BatchSpec{
		AuthorityPaths: []relationobserve.AuthorityPath{authorityPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	planned := commandPlan{
		result: CommandResult{
			ManifestPath:  paths.ManifestPath,
			LockfilePath:  paths.LockfilePath,
			StatefilePath: paths.StatefilePath,
		},
		context: commandContext{
			Paths:     paths,
			Selection: selection,
		},
		assessment: readiness.Assessment{
			StatePath:            paths.StatefilePath,
			RelationObservations: observations,
		},
	}
	evidence, err := buildApplyAuthorityEvidence(t.Context(), planned)
	if err != nil {
		t.Fatal(err)
	}
	want := map[mutation.PathEffect]bool{
		mutation.PathEffectDirectoryEntry: false,
		mutation.PathEffectReferent:       false,
	}
	for _, request := range evidence.revisions {
		if request.Path == inventoryPath {
			want[request.Effect] = true
		}
	}
	for effect, found := range want {
		if !found {
			t.Fatalf("missing relation observation revision for effect %d", effect)
		}
	}
}

func applyAuthorityTestPlan(t *testing.T) commandPlan {
	t.Helper()
	root := t.TempDir()
	selection, err := targetselection.ForDiagnostics([]string{string(target.TargetClaudeCode)})
	if err != nil {
		t.Fatal(err)
	}
	paths := daempaths.Paths{
		ManifestPath:  filepath.Join(root, "daem.toml"),
		ManifestRoot:  root,
		LockfilePath:  filepath.Join(root, "daem.lock.toml"),
		StateDir:      filepath.Join(root, ".daem"),
		StatefilePath: filepath.Join(root, ".daem", "state.json"),
		RecoveryDir:   filepath.Join(root, ".daem", "recovery"),
		DataDir:       filepath.Join(root, "data"),
	}
	environment := desiredtest.Environment(t, desired.Spec{
		Targets:  []target.Target{target.TargetClaudeCode},
		Defaults: desiredtest.Defaults(t, target.ScopeProject, skill.InstallModeCopy),
		Skills: []skill.Skill{desiredtest.Skill(t, skill.Spec{
			Name:        "review",
			Source:      sourcetest.Local(t, "skills/review", source.LocalSourceModeVendor),
			Targets:     []target.Target{target.TargetClaudeCode},
			Scope:       target.ScopeProject,
			InstallMode: skill.InstallModeCopy,
		})},
	})
	planned := commandPlan{
		result: CommandResult{
			ManifestPath:  paths.ManifestPath,
			LockfilePath:  paths.LockfilePath,
			StatefilePath: paths.StatefilePath,
			Reconciliation: applyAuthorityManagedPathPlan(
				t,
				"review",
				"review",
				"desired",
				target.TargetClaudeCode,
				target.ScopeProject,
				ownership.OwnerAuthority{},
				nil,
			),
		},
		assessment: readiness.Assessment{
			StatePath: paths.StatefilePath,
		},
		context: commandContext{
			Paths:              paths,
			RuntimeEnvironment: environment,
			Selection:          selection,
		},
	}
	planned.assessment.Reconciliation = planned.result.Reconciliation.Clone()
	captureApplyProjectRootForTest(t, &planned)
	t.Cleanup(func() { _ = closeCommandPlan(&planned) })
	return planned
}

func applyAuthorityManagedPathPlan(
	t *testing.T,
	name string,
	installName string,
	hashSeed string,
	selectedTarget target.Target,
	scope target.Scope,
	owner ownership.OwnerAuthority,
	ownershipEvidence []observe.OwnershipObservation,
) reconcile.Result {
	t.Helper()
	supply := snapshottest.ExactSupply(t, snapshottest.ExactSupplyInput{
		Kind:         entity.KindSkill,
		Name:         name,
		SourceID:     artifact.SourceID("local:skills/" + name + "?mode=vendor"),
		ArtifactKind: artifact.ArtifactKindDirectory,
		ContentHash:  artifact.HashFileContent([]byte(hashSeed)),
	})
	placements, err := profile.ManagedPathPlacementsFor(
		entity.KindSkill,
		scope,
		[]target.Target{selectedTarget},
	)
	if err != nil || len(placements) != 1 {
		t.Fatalf("ManagedPathPlacementsFor = %#v, %v", placements, err)
	}
	destination, err := placements[0].ChildDestination(installName)
	if err != nil {
		t.Fatal(err)
	}
	writeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationWrite)
	if err != nil {
		t.Fatal(err)
	}
	removeRoute, err := profile.ManagedPathOperationRoute(placements[0], profile.OperationRemove)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := placements[0].Realize(destination, realization.PathProjectionCopy, writeRoute)
	if err != nil {
		t.Fatal(err)
	}
	entityID, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatal(err)
	}
	subjectID, err := topologyprojection.Subject(entityID, placements[0].ID())
	if err != nil {
		t.Fatal(err)
	}
	subject, err := lock.NewManagedPathSubjectContract(lock.ManagedPathSubjectInput{
		EntityID:      entityID,
		SubjectID:     subjectID,
		Realization:   spec,
		WriteRouteID:  writeRoute.RouteID(),
		RemoveRouteID: removeRoute.RouteID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	locked := snapshottest.Section(t, supply, subject)
	admittedSupply, ok := locked.Subject(supply.SubjectID())
	if !ok {
		t.Fatalf("locked section omitted exact Supply %q", supply.SubjectID())
	}
	admittedSubject, ok := locked.Subject(subject.SubjectID())
	if !ok {
		t.Fatalf("locked section omitted managed path %q", subject.SubjectID())
	}
	expectation, err := reconcileprojection.NewManagedPathExpectation(admittedSubject)
	if err != nil {
		t.Fatal(err)
	}
	supplyObservation, err := observe.NewExactSupplyObservation(admittedSupply.SubjectID(), false)
	if err != nil {
		t.Fatal(err)
	}
	pathEvidence, err := observe.NewManagedPathEvidence(
		admittedSubject.SubjectID(),
		destination,
		false,
		"",
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	selectedTargets, err := reconcile.NewSelectedTargets([]target.Target{selectedTarget})
	if err != nil {
		t.Fatal(err)
	}
	decisions, err := reconcileprojection.BuildManagedPathDecisions(reconcileprojection.ManagedPathInput{
		Locked:             locked,
		Expectations:       []reconcileprojection.ManagedPathExpectation{expectation},
		SelectedTargets:    selectedTargets,
		SupplyObservations: []observe.ExactSupplyObservation{supplyObservation},
		Evidence:           []observe.ManagedPathEvidence{pathEvidence},
		Owner:              owner,
		Ownership:          ownershipEvidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathCreate {
		t.Fatalf("managed path decisions = %#v, want one create", decisions)
	}
	return mustReconciliationPlan(t, decisions, nil)
}

func mustReconciliationPlan(
	t testing.TB,
	managedPaths []reconcile.ManagedPathDecision,
	aggregates []reconcile.AggregateDecision,
) reconcile.Result {
	t.Helper()
	result, err := reconcile.NewResult(reconcile.ResultInput{
		Context:      reconcile.ContextApply,
		ManagedPaths: managedPaths,
		Aggregates:   aggregates,
	})
	if err != nil {
		t.Fatalf("NewPlan returned error: %v", err)
	}
	return result
}
