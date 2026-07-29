package execute

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	mcpcodec "github.com/isty2e/daem/internal/realization/aggregate/codec/mcp"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/realization/lock/snapshottest"
	reconcileprojection "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestApplyRollsBackSharedAggregateAfterStatefileFailure(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	beforeContent := []byte(`{
  "mcpServers": {
    "unmanaged": {
      "type": "stdio",
      "command": "keep-me",
      "env": {
        "TOKEN": "SECRET_CANARY"
      }
    }
  },
  "unknownTopLevel": {
    "keep": true
  }
}
`)
	if err := os.WriteFile(configPath, beforeContent, aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}
	effects := managedMCPAggregateEffects(t, aggregate.ExistingDocument(beforeContent), []mcpEffectSpec{
		{serverID: "alpha", command: "alpha-command"},
		{serverID: "beta", command: "beta-command"},
	})
	if len(effects) != 1 || len(effects[0].ProjectionEffects()) != 2 {
		t.Fatalf("effects = %#v, want one document with two projections", effects)
	}
	paths := Paths{
		RecoveryDir:           filepath.Join(dataDir, "recovery"),
		StateDir:              dataDir,
		StatefilePath:         filepath.Join(dataDir, "state.json"),
		ManifestRoot:          root,
		DataDir:               dataDir,
		OwnershipRegistryPath: filepath.Join(dataDir, "ownership.json"),
	}

	var journalCapture []byte
	var journalCaptureErr error
	events := EventSink(func(event Event) {
		if event.Kind != EventStatefileWriteFailed {
			return
		}
		journalCapture, journalCaptureErr = readTreeBytes(paths.RecoveryDir)
	})
	_, err := ApplyWithOptions(context.Background(), ApplyInput{
		Paths:            paths,
		Resolver:         destinationResolver(paths),
		AggregateEffects: effects,
		CurrentState:     durable.EmptySnapshot(),
		Codecs:           testAggregateCodecs(),
		StateCodec:       testStateCodec(),
		Filesystem:       testFilesystem(),
	}, ApplyOptions{
		Events: events,
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{
				status: statefileUncommitted,
				err:    errors.New("injected statefile failure"),
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "injected statefile failure") {
		t.Fatalf("ApplyWithOptions error = %v, want injected statefile failure", err)
	}
	if journalCaptureErr != nil {
		t.Fatalf("read active recovery journal: %v", journalCaptureErr)
	}
	if len(journalCapture) == 0 {
		t.Fatal("statefile failure observed no active recovery journal")
	}
	if strings.Contains(string(journalCapture), "SECRET_CANARY") {
		t.Fatal("recovery journal captured an unmanaged sibling secret")
	}
	beforeDocumentHash := string(artifact.HashFileContent(beforeContent))
	if strings.Contains(string(journalCapture), beforeDocumentHash) {
		t.Fatal("recovery journal captured a value-derived fingerprint of the unmanaged document")
	}

	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	assertJSONMCPEntryAbsent(t, restored, "alpha")
	assertJSONMCPEntryAbsent(t, restored, "beta")
	assertJSONMCPString(t, restored, "mcpServers", "unmanaged", "env", "TOKEN", "SECRET_CANARY")
	assertJSONMCPBool(t, restored, "unknownTopLevel", "keep", true)
	if _, err := os.Stat(paths.StatefilePath); !os.IsNotExist(err) {
		t.Fatalf("statefile stat error = %v, want absent after failed commit", err)
	}
	if entries, err := os.ReadDir(paths.RecoveryDir); err != nil && !os.IsNotExist(err) {
		t.Fatalf("read recovery directory: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("recovery directory retains %d entries after successful rollback", len(entries))
	}
}

func TestApplyRollsBackManagedDirectoryAndAggregateAfterStatefileFailure(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	paths := managedAggregateTestPaths(root, dataDir)
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	beforeConfig := []byte("{\n  \"mcpServers\": {}\n}\n")
	if err := os.WriteFile(configPath, beforeConfig, aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}

	destination := outputtest.Parse(t, ".agents/skills/oracle")
	projection := testManagedPathEffectState(t, "oracle", destination)
	source := filepath.Join(t.TempDir(), "oracle")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatalf("create managed directory payload: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: oracle\n---\n"), 0o600); err != nil {
		t.Fatalf("write managed directory payload: %v", err)
	}
	view, err := access.OpenView(source)
	if err != nil {
		t.Fatalf("open managed directory payload: %v", err)
	}
	desiredHash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("hash managed directory payload: %v", err)
	}
	identity, err := artifact.NewExactIdentity(
		"test:mixed-effect-statefile-failure",
		"",
		artifact.ArtifactKindDirectory,
		desiredHash,
	)
	if err != nil {
		t.Fatalf("construct managed directory identity: %v", err)
	}
	pathEffect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		subject:          projection.Subject(),
		consumerTargets:  projection.ConsumerTargets(),
		scope:            projection.Scope(),
		destination:      destination,
		desiredHash:      desiredHash,
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
	}}}
	directoryPayload, err := payload.NewDirectoryPayload(t.Context(), projection.Subject(), identity, view)
	if err != nil {
		t.Fatalf("construct managed directory payload: %v", err)
	}
	payloads, err := payload.NewPayloadSet([]payload.Payload{directoryPayload}, nil)
	if err != nil {
		t.Fatalf("construct managed directory payload set: %v", err)
	}
	pathEvidence, err := observe.NewManagedPathEvidence(projection.Subject(), destination, false, "", 0)
	if err != nil {
		t.Fatalf("construct managed path evidence: %v", err)
	}
	aggregateEffects := managedMCPAggregateEffects(
		t,
		aggregate.ExistingDocument(beforeConfig),
		[]mcpEffectSpec{{serverID: "mixed", command: "mixed-command"}},
	)

	stateBefore, err := testStateCodec().Encode(durable.EmptySnapshot())
	if err != nil {
		t.Fatalf("encode before state: %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatalf("create data directory: %v", err)
	}
	if err := os.WriteFile(paths.StatefilePath, stateBefore, 0o600); err != nil {
		t.Fatalf("write before statefile: %v", err)
	}

	var completed []ActionEventFacts
	_, err = ApplyWithOptions(t.Context(), ApplyInput{
		Paths: paths, Resolver: destinationResolver(paths),
		ManagedPathEffects:  []ManagedPathEffect{pathEffect},
		ManagedPathEvidence: []observe.ManagedPathEvidence{pathEvidence},
		AggregateEffects:    aggregateEffects,
		CurrentState:        durable.EmptySnapshot(), Payloads: payloads,
		Codecs: testAggregateCodecs(), StateCodec: testStateCodec(), Filesystem: testFilesystem(),
	}, ApplyOptions{
		Events: func(event Event) {
			if event.Kind == EventActionDone && event.Action != nil {
				completed = append(completed, *event.Action)
			}
		},
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{status: statefileUncommitted, err: errors.New("injected mixed-family statefile failure")}
		},
	})
	if err == nil ||
		!strings.Contains(err.Error(), "injected mixed-family statefile failure") ||
		!strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("ApplyWithOptions error = %v, want successful mixed-family rollback", err)
	}
	if len(completed) != 2 ||
		completed[0].Index != 0 || completed[0].ManagedPathKind != ManagedPathEffectCreate ||
		completed[1].Index != 1 || completed[1].AggregateKind != AggregateEffectCreate {
		t.Fatalf("completed action facts = %#v, want distinct managed-path and aggregate indices", completed)
	}
	assertHostMissing(t, filepath.Join(root, filepath.FromSlash(destination.RelativePath())))
	restoredConfig, readErr := os.ReadFile(configPath)
	if readErr != nil || string(restoredConfig) != string(beforeConfig) {
		t.Fatalf("restored aggregate config = %q, error = %v, want %q", restoredConfig, readErr, beforeConfig)
	}
	restoredState, readErr := os.ReadFile(paths.StatefilePath)
	if readErr != nil || string(restoredState) != string(stateBefore) {
		t.Fatalf("restored statefile = %q, error = %v, want exact before state", restoredState, readErr)
	}
	assertNoActiveRecoveryOperation(t, paths.RecoveryDir)
}

func TestApplyRequiresAggregateCodecCatalogBeforeHostEffects(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	beforeContent := []byte("{\"mcpServers\":{}}\n")
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	if err := os.WriteFile(configPath, beforeContent, aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}
	paths := managedAggregateTestPaths(root, dataDir)
	effects := managedMCPAggregateEffects(t, aggregate.ExistingDocument(beforeContent), []mcpEffectSpec{
		{serverID: "alpha", command: "alpha-command"},
	})

	_, err := ApplyWithOptions(context.Background(), ApplyInput{
		Paths:            paths,
		Resolver:         destinationResolver(paths),
		AggregateEffects: effects,
		CurrentState:     durable.EmptySnapshot(),
		StateCodec:       testStateCodec(),
		Filesystem:       testFilesystem(),
	}, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "unsupported recovery aggregate codec") {
		t.Fatalf("ApplyWithOptions error = %v, want missing aggregate codec rejection", err)
	}
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after rejected apply: %v", err)
	}
	if string(after) != string(beforeContent) {
		t.Fatal("apply without codecs changed the host aggregate")
	}
	if _, err := os.Stat(paths.StatefilePath); !os.IsNotExist(err) {
		t.Fatalf("statefile stat error = %v, want absent", err)
	}
	if entries, err := os.ReadDir(paths.RecoveryDir); err != nil && !os.IsNotExist(err) {
		t.Fatalf("read recovery directory: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("recovery directory retains %d entries after rejected apply", len(entries))
	}
}

func TestRecoveryRequiresAggregateCodecCatalogBeforeHostEffects(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	beforeContent := []byte("{\"mcpServers\":{}}\n")
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	if err := os.WriteFile(configPath, beforeContent, aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}
	effects := managedMCPAggregateEffects(t, aggregate.ExistingDocument(beforeContent), []mcpEffectSpec{
		{serverID: "alpha", command: "alpha-command"},
	})
	paths := managedAggregateTestPaths(root, dataDir)

	_, err := ApplyWithOptions(context.Background(), ApplyInput{
		Paths:            paths,
		Resolver:         destinationResolver(paths),
		AggregateEffects: effects,
		CurrentState:     durable.EmptySnapshot(),
		Codecs:           testAggregateCodecs(),
		StateCodec:       testStateCodec(),
		Filesystem:       testFilesystem(),
	}, ApplyOptions{
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{
				status: statefileCommitIndeterminate,
				err:    errors.New("injected indeterminate statefile failure"),
			}
		},
	})
	if err == nil || !strings.Contains(err.Error(), "recovery journal retained") {
		t.Fatalf("ApplyWithOptions error = %v, want retained recovery journal", err)
	}
	afterApply, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read applied config: %v", err)
	}
	assertJSONMCPString(t, afterApply, "mcpServers", "alpha", "command", "alpha-command")

	recoveryPlan, err := loadActivePlanWithTestCodecs(context.Background(), paths)
	if err != nil {
		t.Fatalf("load recovery plan: %v", err)
	}
	if recoveryPlan.Classification() != recovery.ClassificationNeedsRollback {
		t.Fatalf("recovery classification = %q, want rollback", recoveryPlan.Classification())
	}

	beforeHostActions := 0
	err = ExecuteRecoveryPlanWithOptions(context.Background(), recoveryPlan, paths, RecoveryOptions{
		Resolver:    destinationResolver(paths),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
		Filesystem:  testFilesystem(),
		beforeHostAction: func(int) error {
			beforeHostActions++
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "recovery is blocked by current evidence") {
		t.Fatalf("recovery without codecs error = %v, want evidence block", err)
	}
	if beforeHostActions != 0 {
		t.Fatalf("recovery attempted %d host actions without an aggregate codec catalog", beforeHostActions)
	}
	afterBlockedRecovery, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config after blocked recovery: %v", err)
	}
	if string(afterBlockedRecovery) != string(afterApply) {
		t.Fatal("blocked recovery changed the host aggregate")
	}
	if entries, err := os.ReadDir(paths.RecoveryDir); err != nil || len(entries) == 0 {
		t.Fatalf("blocked recovery journal entries = %d, error = %v; want retained evidence", len(entries), err)
	}

	if err := ExecuteRecoveryPlanWithOptions(context.Background(), recoveryPlan, paths, RecoveryOptions{
		Resolver:    destinationResolver(paths),
		Codecs:      testAggregateCodecs(),
		StateCodec:  testStateCodec(),
		StateReader: testStateReader(paths.StatefilePath),
		Filesystem:  testFilesystem(),
	}); err != nil {
		t.Fatalf("recovery with codecs returned error: %v", err)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	assertJSONMCPEntryAbsent(t, restored, "alpha")
	assertNoActiveRecoveryOperation(t, paths.RecoveryDir)
}

func TestApplyRollsBackMixedAggregateProjectionRowsAfterStatefileFailure(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	alphaBefore := managedMCPContract(t, "alpha", "alpha-old")
	alphaAfter := managedMCPContract(t, "alpha", "alpha-new")
	beta := managedMCPContract(t, "beta", "beta-command")
	gamma := managedMCPContract(t, "gamma", "gamma-command")
	adopt := managedMCPContract(t, "adopt", "adopt-command")

	beforeItems := managedAggregateItems(t, alphaBefore, beta, gamma, adopt)
	before := aggregateDocumentWithContributions(
		t,
		aggregate.ExistingDocument([]byte(`{
  "mcpServers": {
    "unmanaged": {
      "type": "stdio",
      "command": "keep-me",
      "env": {
        "TOKEN": "SECRET_CANARY"
      }
    }
  },
  "unknownTopLevel": {
    "keep": true
  }
}
`)),
		beforeItems,
	)
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	if err := os.WriteFile(configPath, before.Content(), aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}

	desiredItems := managedAggregateItems(t, alphaAfter, gamma, adopt)
	previousItems := managedAggregateItems(t, alphaBefore, beta, gamma)
	decisions, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked:                 managedLockedSection(t, alphaAfter, gamma, adopt),
		Expected:               []lock.LockedSubjectContract{alphaAfter, gamma, adopt},
		Desired:                desiredItems,
		States:                 managedAggregateStates(t, previousItems),
		Evidence:               []observe.AggregateEvidence{managedAggregateEvidence(t, beforeItems, before)},
		SelectedTargets:        testSelectedTargets(t, target.TargetClaudeCode),
		ManageUnmanagedMatches: true,
		Codecs:                 testAggregateCodecs(),
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	effects, err := AggregateEffects(decisions)
	if err != nil {
		t.Fatalf("AggregateEffects returned error: %v", err)
	}
	if len(effects) != 1 {
		t.Fatalf("effects = %d, want one document effect", len(effects))
	}
	projectionKinds := make(map[string]AggregateEffectKind)
	for _, projection := range effects[0].ProjectionEffects() {
		projectionKinds[string(projection.Contract().Address().ContentPath())] = projection.Kind()
	}
	for contentPath, want := range map[string]AggregateEffectKind{
		"/mcpServers/alpha": AggregateEffectReplace,
		"/mcpServers/adopt": AggregateEffectRecord,
		"/mcpServers/beta":  AggregateEffectRemove,
		"/mcpServers/gamma": AggregateEffectRecord,
	} {
		if got := projectionKinds[contentPath]; got != want {
			t.Fatalf("projection %q kind = %q, want %q", contentPath, got, want)
		}
	}
	mutations, err := aggregateJournalMutations(effects)
	if err != nil {
		t.Fatalf("aggregateJournalMutations returned error: %v", err)
	}
	if len(mutations) != 3 {
		t.Fatalf("journal mutations = %d, want replace, record, and remove rows", len(mutations))
	}
	currentState, err := durable.NewSnapshot(durable.SnapshotInput{
		ManagedAggregates: managedAggregateStates(t, previousItems),
	})
	if err != nil {
		t.Fatal(err)
	}
	paths := Paths{
		RecoveryDir:           filepath.Join(dataDir, "recovery"),
		StateDir:              dataDir,
		StatefilePath:         filepath.Join(dataDir, "state.json"),
		ManifestRoot:          root,
		DataDir:               dataDir,
		OwnershipRegistryPath: filepath.Join(dataDir, "ownership.json"),
	}
	_, err = ApplyWithOptions(context.Background(), ApplyInput{
		Paths: paths, Resolver: destinationResolver(paths),
		AggregateEffects: effects, CurrentState: currentState, Codecs: testAggregateCodecs(),
		StateCodec: testStateCodec(), Filesystem: testFilesystem(),
	}, ApplyOptions{
		commitStatefile: func(context.Context, string, []byte, os.FileMode) statefileCommitOutcome {
			return statefileCommitOutcome{
				status: statefileUncommitted,
				err:    errors.New("injected mixed statefile failure"),
			}
		},
	})
	if err == nil ||
		!strings.Contains(err.Error(), "injected mixed statefile failure") ||
		!strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("ApplyWithOptions error = %v, want successful guarded rollback", err)
	}
	restored, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatalf("read restored config: %v", readErr)
	}
	if string(restored) != string(before.Content()) {
		t.Fatalf("restored config differs from exact before document:\n%s\nwant:\n%s", restored, before.Content())
	}
}

func TestAggregateCancellationAfterJournalLeavesHostUntouched(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	beforeContent := []byte(`{"mcpServers":{"unmanaged":{"type":"stdio","command":"keep-me"}}}`)
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	if err := os.WriteFile(configPath, beforeContent, aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}
	effects := managedMCPAggregateEffects(t, aggregate.ExistingDocument(beforeContent), []mcpEffectSpec{
		{serverID: "alpha", command: "alpha-command"},
		{serverID: "beta", command: "beta-command"},
	})
	paths := managedAggregateTestPaths(root, dataDir)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, ApplyInput{
		Paths:            paths,
		Resolver:         destinationResolver(paths),
		AggregateEffects: effects,
		CurrentState:     durable.EmptySnapshot(),
		Codecs:           testAggregateCodecs(),
		StateCodec:       testStateCodec(),
		Filesystem:       testFilesystem(),
	}, ApplyOptions{
		Events: func(event Event) {
			if event.Kind == EventJournalCaptured {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ApplyWithOptions error = %v, want context cancellation", err)
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil || string(after) != string(beforeContent) {
		t.Fatalf("config after journal cancellation = %q, error = %v, want exact before content", after, readErr)
	}
	assertNoActiveRecoveryOperation(t, paths.RecoveryDir)
}

func TestAggregateCancellationAfterVisibleBatchRollsBackEveryProjection(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, ".daem")
	beforeContent := []byte(`{
  "mcpServers": {
    "unmanaged": {
      "type": "stdio",
      "command": "keep-me",
      "env": {
        "TOKEN": "SECRET_CANARY"
      }
    }
  }
}
`)
	configPath := filepath.Join(root, aggregate.ClaudeProjectMCPConfigPath)
	if err := os.WriteFile(configPath, beforeContent, aggregate.DocumentFileMode); err != nil {
		t.Fatalf("write before config: %v", err)
	}
	effects := managedMCPAggregateEffects(t, aggregate.ExistingDocument(beforeContent), []mcpEffectSpec{
		{serverID: "alpha", command: "alpha-command"},
		{serverID: "beta", command: "beta-command"},
	})
	paths := managedAggregateTestPaths(root, dataDir)
	ctx, cancel := context.WithCancel(context.Background())
	_, err := ApplyWithOptions(ctx, ApplyInput{
		Paths:            paths,
		Resolver:         destinationResolver(paths),
		AggregateEffects: effects,
		CurrentState:     durable.EmptySnapshot(),
		Codecs:           testAggregateCodecs(),
		StateCodec:       testStateCodec(),
		Filesystem:       testFilesystem(),
	}, ApplyOptions{
		Events: func(event Event) {
			if event.Kind == EventActionDone {
				cancel()
			}
		},
	})
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), "host changes rolled back") {
		t.Fatalf("ApplyWithOptions error = %v, want canceled batch with guarded rollback", err)
	}
	after, readErr := os.ReadFile(configPath)
	if readErr != nil || string(after) != string(beforeContent) {
		t.Fatalf("config after visible batch cancellation = %q, error = %v, want exact before content", after, readErr)
	}
	assertNoActiveRecoveryOperation(t, paths.RecoveryDir)
}

type mcpEffectSpec struct {
	serverID string
	command  string
}

func managedMCPAggregateEffects(
	t *testing.T,
	before aggregate.Document,
	specs []mcpEffectSpec,
) []AggregateEffect {
	t.Helper()
	contracts := make([]lock.LockedSubjectContract, 0, len(specs))
	desired := make([]aggregate.SubjectContribution, 0, len(specs))
	projectionContracts := make([]aggregate.ProjectionContract, 0, len(specs))
	for _, spec := range specs {
		canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(
			mcpcodec.ClaudeProjectMCPServerProjection{
				ServerID:        spec.serverID,
				Command:         spec.command,
				AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
			},
		)
		if err != nil {
			t.Fatalf("canonical MCP entry %q: %v", spec.serverID, err)
		}
		contract := snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
			PlacementID:         aggregate.MCPPlacementClaudeProject,
			ServerID:            spec.serverID,
			LauncherCommand:     spec.command,
			CanonicalProjection: string(canonical),
		})
		item, present, err := contract.ManagedAggregateContribution()
		if err != nil || !present {
			t.Fatalf("aggregate contribution %q = %#v, %t, %v", spec.serverID, item, present, err)
		}
		contracts = append(contracts, contract)
		desired = append(desired, item)
		projectionContracts = append(projectionContracts, item.Contribution().Contract())
	}
	locked, err := lock.NewLockedSection(contracts, nil)
	if err != nil {
		t.Fatalf("NewLockedSection returned error: %v", err)
	}
	selection, err := aggregate.NewSelection(projectionContracts)
	if err != nil {
		t.Fatalf("NewSelection returned error: %v", err)
	}
	codec, present := testAggregateCodecs().Lookup(selection.CodecContractID())
	if !present {
		t.Fatalf("codec %q is not admitted", selection.CodecContractID())
	}
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatalf("codec Read returned failure: %v", failure)
	}
	evidence, err := observe.NewAggregateEvidence(before, snapshot, aggregate.DocumentFileMode)
	if err != nil {
		t.Fatalf("NewAggregateEvidence returned error: %v", err)
	}
	decisions, err := reconcileprojection.BuildAggregateDecisions(reconcileprojection.AggregateInput{
		Locked:          locked,
		Expected:        contracts,
		Desired:         desired,
		Evidence:        []observe.AggregateEvidence{evidence},
		SelectedTargets: testSelectedTargets(t, target.TargetClaudeCode),
		Codecs:          testAggregateCodecs(),
	})
	if err != nil {
		t.Fatalf("BuildAggregateDecisions returned error: %v", err)
	}
	effects, err := AggregateEffects(decisions)
	if err != nil {
		t.Fatalf("AggregateEffects returned error: %v", err)
	}
	return effects
}

func managedAggregateTestPaths(root string, dataDir string) Paths {
	return Paths{
		RecoveryDir:           filepath.Join(dataDir, "recovery"),
		StateDir:              dataDir,
		StatefilePath:         filepath.Join(dataDir, "state.json"),
		ManifestRoot:          root,
		DataDir:               dataDir,
		OwnershipRegistryPath: filepath.Join(dataDir, "ownership.json"),
	}
}

func managedMCPContract(
	t *testing.T,
	serverID string,
	command string,
) lock.LockedSubjectContract {
	t.Helper()
	canonical, err := mcpcodec.CanonicalClaudeProjectMCPServerEntry(
		mcpcodec.ClaudeProjectMCPServerProjection{
			ServerID:        serverID,
			Command:         command,
			AdapterContract: aggregate.ClaudeProjectMCPStdioAdapterV1,
		},
	)
	if err != nil {
		t.Fatalf("canonical MCP entry %q: %v", serverID, err)
	}
	return snapshottest.MCPProjection(t, snapshottest.MCPProjectionInput{
		PlacementID:         aggregate.MCPPlacementClaudeProject,
		ServerID:            serverID,
		LauncherCommand:     command,
		CanonicalProjection: string(canonical),
	})
}

func managedAggregateItems(
	t *testing.T,
	contracts ...lock.LockedSubjectContract,
) []aggregate.SubjectContribution {
	t.Helper()
	items := make([]aggregate.SubjectContribution, 0, len(contracts))
	for _, contract := range contracts {
		item, present, err := contract.ManagedAggregateContribution()
		if err != nil || !present {
			t.Fatalf("ManagedAggregateContribution = %#v, %t, %v", item, present, err)
		}
		items = append(items, item)
	}
	return items
}

func managedAggregateStates(
	t *testing.T,
	items []aggregate.SubjectContribution,
) []durable.ManagedAggregateState {
	t.Helper()
	states := make([]durable.ManagedAggregateState, 0, len(items))
	for _, item := range items {
		state, err := durable.NewManagedAggregateState(item.SubjectID(), item.Contribution())
		if err != nil {
			t.Fatal(err)
		}
		states = append(states, state)
	}
	return states
}

func managedLockedSection(
	t *testing.T,
	contracts ...lock.LockedSubjectContract,
) lock.LockedSection {
	t.Helper()
	locked, err := lock.NewLockedSection(contracts, nil)
	if err != nil {
		t.Fatal(err)
	}
	return locked
}

func aggregateDocumentWithContributions(
	t *testing.T,
	before aggregate.Document,
	items []aggregate.SubjectContribution,
) aggregate.Document {
	t.Helper()
	contracts := make([]aggregate.ProjectionContract, len(items))
	for index, item := range items {
		contracts[index] = item.Contribution().Contract()
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	codec, present := testAggregateCodecs().Lookup(selection.CodecContractID())
	if !present {
		t.Fatalf("codec %q is not admitted", selection.CodecContractID())
	}
	snapshot, failure := codec.Read(before, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	statesByAddress := make(map[aggregate.ProjectionAddress]aggregate.ProjectionState, len(items))
	for _, state := range snapshot.States() {
		statesByAddress[state.Contract().Address()] = state
	}
	intents := make([]aggregate.ProjectionIntent, 0, len(items))
	for _, item := range items {
		set, err := aggregate.NewContributionSet([]aggregate.SubjectContribution{item})
		if err != nil {
			t.Fatal(err)
		}
		state, present := statesByAddress[item.Contribution().Contract().Address()]
		if !present {
			t.Fatalf(
				"snapshot does not cover contribution address %#v",
				item.Contribution().Contract().Address(),
			)
		}
		intent, err := aggregate.NewProjectionIntent(state, &set)
		if err != nil {
			t.Fatal(err)
		}
		intents = append(intents, intent)
	}
	codecPlan, err := aggregate.NewPlan(snapshot, intents)
	if err != nil {
		t.Fatal(err)
	}
	rendered, failure := codec.Render(before, codecPlan)
	if failure != nil {
		t.Fatal(failure)
	}
	return rendered.Document()
}

func managedAggregateEvidence(
	t *testing.T,
	items []aggregate.SubjectContribution,
	document aggregate.Document,
) observe.AggregateEvidence {
	t.Helper()
	contracts := make([]aggregate.ProjectionContract, len(items))
	for index, item := range items {
		contracts[index] = item.Contribution().Contract()
	}
	selection, err := aggregate.NewSelection(contracts)
	if err != nil {
		t.Fatal(err)
	}
	codec, present := testAggregateCodecs().Lookup(selection.CodecContractID())
	if !present {
		t.Fatalf("codec %q is not admitted", selection.CodecContractID())
	}
	snapshot, failure := codec.Read(document, selection)
	if failure != nil {
		t.Fatal(failure)
	}
	evidence, err := observe.NewAggregateEvidence(document, snapshot, aggregate.DocumentFileMode)
	if err != nil {
		t.Fatal(err)
	}
	return evidence
}

func readTreeBytes(root string) ([]byte, error) {
	var content []byte
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		value, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content = append(content, value...)
		return nil
	})
	return content, err
}

func assertJSONMCPEntryAbsent(t *testing.T, content []byte, serverID string) {
	t.Helper()
	root := decodeJSONTree(t, content)
	servers := root["mcpServers"].(map[string]any)
	if _, present := servers[serverID]; present {
		t.Fatalf("MCP entry %q is still present", serverID)
	}
}

func assertJSONMCPString(t *testing.T, content []byte, path ...string) {
	t.Helper()
	if len(path) < 2 {
		t.Fatal("JSON string assertion requires a path and expected value")
	}
	want := path[len(path)-1]
	current := any(decodeJSONTree(t, content))
	for _, part := range path[:len(path)-1] {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("JSON path %q reached non-object %#v", strings.Join(path, "."), current)
		}
		current, ok = object[part]
		if !ok {
			t.Fatalf("JSON path %q is absent", strings.Join(path, "."))
		}
	}
	if current != want {
		t.Fatalf("JSON path %q = %#v, want %q", strings.Join(path, "."), current, want)
	}
}

func assertJSONMCPBool(t *testing.T, content []byte, first string, second string, want bool) {
	t.Helper()
	root := decodeJSONTree(t, content)
	object, ok := root[first].(map[string]any)
	if !ok {
		t.Fatalf("JSON object %q = %#v", first, root[first])
	}
	if got, ok := object[second].(bool); !ok || got != want {
		t.Fatalf("JSON path %q.%q = %#v, want %t", first, second, object[second], want)
	}
}

func decodeJSONTree(t *testing.T, content []byte) map[string]any {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(content, &root); err != nil {
		t.Fatalf("decode JSON config: %v", err)
	}
	return root
}
