package apply

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	effectpreparation "github.com/isty2e/daem/internal/effect/execute/preparation"

	liveobserve "github.com/isty2e/daem/internal/assurance/observe/live"
	lockobserve "github.com/isty2e/daem/internal/assurance/observe/lock"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/effect/execute"
	journal "github.com/isty2e/daem/internal/effect/journal/publication"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	payloadbuild "github.com/isty2e/daem/internal/effect/payload/build"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/realization/lockfile"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/reconcile"
	projectionbuild "github.com/isty2e/daem/internal/reconcile/build/projection"
	"github.com/isty2e/daem/internal/supply/source"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	"github.com/isty2e/daem/internal/topology"
)

func TestNativeDocumentRealSourceObservationPreparesWholeFileEffect(t *testing.T) {
	for _, selected := range []target.Target{target.TargetPi, target.TargetCodex} {
		t.Run(string(selected), func(t *testing.T) {
			root := t.TempDir()
			sourcePath := filepath.Join(root, "source.md")
			original := []byte("# actual whole document\r\nliteral content\n")
			if err := os.WriteFile(sourcePath, original, 0o600); err != nil {
				t.Fatal(err)
			}
			spec, err := source.NewLocalSource(sourcePath, source.LocalSourceModeVendor)
			if err != nil {
				t.Fatal(err)
			}
			config, err := hostconfig.New(hostconfig.Spec{Name: "document", Source: spec, Target: selected, Scope: target.ScopeProject, Path: "prompts/review.md", Ownership: hostconfig.OwnershipDocument})
			if err != nil {
				t.Fatal(err)
			}
			defaults, err := desired.NewDefaults(target.ScopeProject, desiredskill.InstallModeCopy)
			if err != nil {
				t.Fatal(err)
			}
			environment, err := desired.New(desired.Spec{Targets: []target.Target{selected}, Defaults: defaults, HostConfigs: []hostconfig.Config{config}})
			if err != nil {
				t.Fatal(err)
			}
			paths, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
			if err != nil {
				t.Fatal(err)
			}
			resolver, err := sourceresolution.NewResolver(paths)
			if err != nil {
				t.Fatal(err)
			}
			built, err := lockbuild.BuildNativeDocumentsWithOptions(t.Context(), environment, resolver, lockbuild.Options{}, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := lockfile.Marshal(built)
			if err != nil {
				t.Fatal(err)
			}
			lockPath := filepath.Join(root, "native.lock.toml")
			if err := os.WriteFile(lockPath, encoded, 0o600); err != nil {
				t.Fatal(err)
			}
			locked, err := lockfile.Load(t.Context(), lockPath)
			if err != nil {
				t.Fatal(err)
			}
			selection, err := targetselection.ForAvailableTargets(environment.Targets(), []string{string(selected)})
			if err != nil {
				t.Fatal(err)
			}
			selectedTargets, err := reconcile.NewSelectedTargets(selection.Targets())
			if err != nil {
				t.Fatal(err)
			}
			placement, err := profile.ResolveHostConfigPlacement(configuration.PlacementInput{Target: selected, Scope: config.Scope(), Path: config.Path(), Ownership: config.Ownership(), Syntax: config.Syntax()})
			if err != nil {
				t.Fatal(err)
			}
			expected, err := lock.NewHostConfigDocumentSubjectContract(config.ID(), placement)
			if err != nil {
				t.Fatal(err)
			}
			expectation, err := projectionbuild.NewManagedPathExpectation(expected)
			if err != nil {
				t.Fatal(err)
			}
			stateCodec, err := statefile.NewNativeCodec(aggregatecodec.Catalog().Contributions())
			if err != nil {
				t.Fatal(err)
			}
			current, err := statefile.LoadOptionalWithOptions(t.Context(), paths.StatefilePath, statefile.ReadOptions{Codec: stateCodec, PhysicalPathBudget: physicalwork.NewPathBudget()})
			if err != nil {
				t.Fatal(err)
			}
			live, err := liveobserve.ManagedPathEvidence(t.Context(), func(destination output.Destination) (string, error) {
				return filepath.Join(root, filepath.FromSlash(destination.RelativePath())), nil
			}, []liveobserve.ManagedPathRequest{{Subject: expected.SubjectID(), Destination: placement.Destination(), ContentKind: realization.PathProjectionFile}})
			if err != nil {
				t.Fatal(err)
			}
			decisionInput := projectionbuild.ManagedPathInput{Locked: locked.Locked, Expectations: []projectionbuild.ManagedPathExpectation{expectation}, SelectedTargets: selectedTargets, States: current.ManagedPaths(), Evidence: live}
			observeDecisions := func() []reconcile.ManagedPathDecision {
				t.Helper()
				epoch, err := lockobserve.ResolveNativeDocumentSourceEpoch(t.Context(), paths, environment, locked, selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
				if err != nil {
					t.Fatal(err)
				}
				observations, err := epoch.Observations(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				decisionInput.SupplyObservations = observations.ExactSupplies()
				decisions, err := projectionbuild.BuildManagedPathDecisions(decisionInput)
				if err != nil {
					t.Fatal(err)
				}
				return decisions
			}
			decisions := observeDecisions()
			if len(decisions) != 1 || decisions[0].Kind() != reconcile.ManagedPathCreate {
				t.Fatalf("real source/live evidence did not prepare native creation: %#v", decisions)
			}
			effects, err := effectpreparation.ManagedPathEffects(decisions)
			if err != nil {
				t.Fatal(err)
			}
			if len(effects) != 1 || effects[0].Subject() != expected.SubjectID() || effects[0].DesiredFileMode() != 0o600 {
				t.Fatal("closed whole-file effect lost native identity/private mode")
			}
			payloads, err := payloadbuild.NativeDocumentPayloadSet(t.Context(), payloadbuild.Input{Paths: paths, Environment: environment, Lockfile: locked, Selection: selection, ManagedPathPayloadSubjects: []topology.SubjectID{effects[0].Subject()}})
			if err != nil {
				t.Fatal(err)
			}
			defer payloads.Cleanup()
			value, present := payloads.LookupSubject(effects[0].Subject())
			file, regular := value.File()
			if !present || !regular || !bytes.Equal(file.Bytes(), original) || value.Hash() != effects[0].DesiredHash() {
				t.Fatal("real source payload does not match prepared effect")
			}
			sourceEpoch, err := lockobserve.ResolveNativeDocumentSourceEpoch(t.Context(), paths, environment, locked, selection, configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64})
			if err != nil {
				t.Fatal(err)
			}
			observedSource, err := sourceEpoch.NativeSource(t.Context(), config)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := effectpreparation.PrepareNativeApplyState(t.Context(), effectpreparation.NativeApplyStateInput{Before: current, ManagedPaths: decisions, WholeSources: []lock.LockedSubjectContract{observedSource.SourceContract()}, StateCodec: stateCodec})
			if err != nil || len(prepared.AfterState().ManagedPaths()) != 1 || prepared.AfterState().ManagedPaths()[0].ContentHash() != effects[0].DesiredHash() || prepared.AfterState().ManagedPaths()[0].FileMode() != 0o600 {
				t.Fatal("actual native whole source lost positive native state preparation", err)
			}
			if _, err := execute.PrepareApplyEffectPlan(execute.ApplyInput{ManagedPathEffects: effects, CurrentState: current, Payloads: payloads, StateCodec: stateCodec}); !errors.Is(err, effectpreparation.ErrNativeOperationRequired) {
				t.Fatal("actual native whole effect entered ordinary structural preparation", err)
			}
			mutations, err := effectpreparation.ManagedPathJournalMutations(effects)
			if err != nil || len(mutations) != 1 {
				t.Fatalf("actual native effect cannot enter capture preparation: %v", err)
			}
			comparison, err := journal.PrepareWholeDocument(t.Context(), journal.WholeDocumentPreparationInput{
				Before: aggregate.AbsentDocument(), Limits: configdocument.Limits{MaximumBytes: 1 << 20, MaximumDepth: 64},
			}, mutations[0], payloads)
			if err != nil {
				t.Fatal(err)
			}
			beforeMatch, err := comparison.Match(t.Context(), aggregate.AbsentDocument(), nil)
			if err != nil || !beforeMatch.MatchesBefore() || beforeMatch.MatchesExpected() {
				t.Fatalf("capture preparation lost actual original absence: %+v, %v", beforeMatch, err)
			}
			expectedMatch, err := comparison.Match(t.Context(), aggregate.ExistingDocument(original), recovery.NewPermissionMode(file.Mode()))
			if err != nil || !expectedMatch.MatchesExpected() || expectedMatch.MatchesBefore() {
				t.Fatalf("capture preparation lost actual prepared source bytes/mode: %+v, %v", expectedMatch, err)
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(placement.Destination().RelativePath()))); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("preparation crossed host-write boundary: %v", err)
			}
			if err := os.WriteFile(sourcePath, []byte("# changed after lock\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			stale := observeDecisions()
			if len(stale) != 1 || !stale[0].IsBlocked() || stale[0].Reason() != reconcile.ReasonStaleLock {
				t.Fatal("changed real source did not block native effect preparation")
			}
			if leaked, err := effectpreparation.ManagedPathEffects(stale); err == nil || len(leaked) != 0 {
				t.Fatal("stale actual source produced a runnable effect")
			}
		})
	}
}
