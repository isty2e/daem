package execute

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/reconcile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyhook "github.com/isty2e/daem/internal/topology/hook"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func assertCommittedApplyResult(
	t *testing.T,
	result ApplyResult,
	statePath string,
	committedStatePath string,
	wantActionCount int,
) {
	t.Helper()
	if result.ActionCount != wantActionCount || result.StatePath != statePath {
		t.Fatalf(
			"ApplyResult = actions=%d state_path=%q, want %d/%q",
			result.ActionCount,
			result.StatePath,
			wantActionCount,
			statePath,
		)
	}
	committed, err := statefile.Load(t.Context(), committedStatePath)
	if err != nil {
		t.Fatalf("load committed statefile: %v", err)
	}
	if !result.State.Equal(committed) {
		t.Fatal("ApplyResult state does not equal the committed statefile")
	}
}

func containsApplyEventKind(events []Event, kind EventKind) bool {
	for _, event := range events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

type applyEventFixture struct {
	root         string
	paths        Paths
	current      durable.Snapshot
	payloads     []payload.Payload
	destinations map[string]output.Destination
}

func newApplyEventFixture(t *testing.T) *applyEventFixture {
	t.Helper()

	root := t.TempDir()
	return &applyEventFixture{
		root: root,
		paths: Paths{
			RecoveryDir:   filepath.Join(root, ".daem", "recovery"),
			StateDir:      filepath.Join(root, ".daem"),
			StatefilePath: filepath.Join(root, ".daem", "state.json"),
			ManifestRoot:  root,
		},
		current:      durable.EmptySnapshot(),
		destinations: make(map[string]output.Destination),
	}
}

type applyEventPreviousState struct {
	Subject     topology.SubjectID
	Scope       target.Scope
	Destination output.Destination
	ContentHash artifact.ContentHash
}

type applyEventAction struct {
	Kind            reconcile.ActionKind
	Reason          reconcile.ActionReason
	Subject         topology.SubjectID
	Scope           target.Scope
	Destination     output.Destination
	DesiredHash     artifact.ContentHash
	LivePathHash    artifact.ContentHash
	LiveFileMode    os.FileMode
	DesiredFileMode os.FileMode
	PreviousState   *applyEventPreviousState
}

func (fixture *applyEventFixture) input(actions []applyEventAction) ApplyInput {
	effects, evidence := fixture.managedPathInputs(actions)
	payloads, err := payload.NewPayloadSet(fixture.payloads, nil)
	if err != nil {
		panic(err)
	}
	return ApplyInput{
		Paths:               fixture.paths,
		Resolver:            destinationResolver(fixture.paths),
		ManagedPathEffects:  effects,
		ManagedPathEvidence: evidence,
		CurrentState:        fixture.current,
		Payloads:            payloads,
		StateCodec:          testStateCodec(),
		Filesystem:          testFilesystem(),
	}
}

func (fixture *applyEventFixture) managedPathInputs(actions []applyEventAction) ([]ManagedPathEffect, []observe.ManagedPathEvidence) {
	effects := make([]ManagedPathEffect, 0, len(actions))
	evidence := make([]observe.ManagedPathEvidence, 0, len(actions)+1)
	for _, action := range actions {
		desiredMode := action.DesiredFileMode.Perm()
		if desiredMode == 0 && action.Kind != reconcile.ActionKindDelete {
			desiredMode = 0o600
		}
		liveMode := action.LiveFileMode.Perm()
		if liveMode == 0 && action.Kind != reconcile.ActionKindCreate {
			liveMode = 0o600
		}

		var previous *durable.ManagedPathState
		if action.PreviousState != nil {
			state, err := durable.NewManagedPathState(
				action.PreviousState.Subject,
				[]target.Target{target.TargetCodex},
				action.PreviousState.Scope,
				action.PreviousState.Destination,
				action.PreviousState.ContentHash,
				realization.PathProjectionFile,
				realization.PathPermissionsExact,
				liveMode,
			)
			if err != nil {
				panic(err)
			}
			previous = &state
		}

		facts := managedPathEffectFacts{
			subject: action.Subject, consumerTargets: []target.Target{target.TargetCodex},
			scope: action.Scope, destination: action.Destination,
			desiredHash: action.DesiredHash, liveHash: action.LivePathHash,
			contentKind:      realization.PathProjectionFile,
			permissionPolicy: realization.PathPermissionsExact,
			desiredFileMode:  desiredMode, liveFileMode: liveMode, previous: previous,
		}
		var effect ManagedPathEffect
		switch action.Kind {
		case reconcile.ActionKindCreate:
			effect.create = &managedPathCreateEffect{facts: facts}
		case reconcile.ActionKindUpdate:
			effect.replace = &managedPathReplaceEffect{facts: facts}
		case reconcile.ActionKindDelete:
			facts.consumerTargets = nil
			effect.remove = &managedPathRemoveEffect{facts: facts}
		case reconcile.ActionKindRecord:
			effect.record = &managedPathRecordEffect{facts: facts}
		default:
			panic("unsupported apply event fixture action")
		}
		if err := effect.validate(); err != nil {
			panic(err)
		}
		effects = append(effects, effect)

		if previous != nil && previous.Destination() != action.Destination {
			absent, err := observe.NewManagedPathEvidence(action.Subject, action.Destination, false, "", 0)
			if err != nil {
				panic(err)
			}
			evidence = append(evidence, absent)
			present, err := observe.NewManagedPathEvidence(
				action.Subject, previous.Destination(), true, previous.ContentHash(), liveMode,
			)
			if err != nil {
				panic(err)
			}
			evidence = append(evidence, present)
			continue
		}
		exists := action.Kind != reconcile.ActionKindCreate
		liveHash := action.LivePathHash
		if !exists {
			liveHash = ""
			liveMode = 0
		}
		observed, err := observe.NewManagedPathEvidence(action.Subject, action.Destination, exists, liveHash, liveMode)
		if err != nil {
			panic(err)
		}
		evidence = append(evidence, observed)
	}
	return effects, evidence
}

func (fixture *applyEventFixture) applyWithEvents(t *testing.T, actions []applyEventAction) []Event {
	t.Helper()

	var events []Event
	result, err := ApplyWithOptions(context.Background(), fixture.input(actions), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("ApplyWithOptions returned error: %v", err)
	}
	if result.ActionCount != len(actions) || result.StatePath != fixture.paths.StatefilePath {
		t.Fatalf("result = %#v, want action count %d and state path %q", result, len(actions), fixture.paths.StatefilePath)
	}
	return events
}

func (fixture *applyEventFixture) applyExpectError(action applyEventAction) ([]Event, error) {
	var events []Event
	_, err := ApplyWithOptions(context.Background(), fixture.input([]applyEventAction{action}), ApplyOptions{
		Events: func(event Event) {
			events = append(events, event)
		},
	})
	return events, err
}

func (fixture *applyEventFixture) createAction(name string, destination string, content string) applyEventAction {
	subject := applyTestSubject(name)
	hash := artifact.HashFileContent([]byte(content))
	canonicalDestination := fixture.hookAssetDestination(name, hash)
	fixture.destinations[destination] = canonicalDestination
	fixture.appendFilePayload(subject, content, 0o600)
	return applyEventAction{
		Kind:            reconcile.ActionKindCreate,
		Reason:          reconcile.ReasonMissingOutput,
		Subject:         subject,
		Scope:           target.ScopeProject,
		Destination:     canonicalDestination,
		DesiredHash:     hash,
		DesiredFileMode: 0o600,
	}
}

func (fixture *applyEventFixture) updateAction(name string, destination string, oldContent string, newContent string) applyEventAction {
	action := fixture.existingManagedAction(reconcile.ActionKindUpdate, name, destination, oldContent)
	newHash := artifact.HashFileContent([]byte(newContent))
	newDestination := fixture.hookAssetDestination(name, newHash)
	fixture.destinations[destination] = newDestination
	action.Reason = reconcile.ReasonContentChanged
	action.Destination = newDestination
	action.DesiredHash = newHash
	action.DesiredFileMode = 0o600
	fixture.appendFilePayload(action.Subject, newContent, 0o600)
	return action
}

func (fixture *applyEventFixture) updateExecutableFileAction(t *testing.T, name string, destination string, oldContent string, newContent string) applyEventAction {
	t.Helper()

	subject := applyTestSubject(name)
	oldHash := artifact.HashFileContentWithExecutable([]byte(oldContent), true)
	newHash := artifact.HashFileContentWithExecutable([]byte(newContent), true)
	oldDestination := fixture.hookAssetDestination(name, oldHash)
	newDestination := fixture.hookAssetDestination(name, newHash)
	fixture.destinations[destination] = newDestination
	writeApplyEventFile(nil, fixture.hostPath(oldDestination.String()), oldContent)
	if err := os.Chmod(fixture.hostPath(oldDestination.String()), 0o700); err != nil {
		t.Fatalf("chmod executable hook asset: %v", err)
	}
	fixture.appendManagedPath(durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		oldDestination,
		oldHash,
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o700,
	))
	fixture.appendFilePayload(subject, newContent, 0o700)
	return applyEventAction{
		Kind:            reconcile.ActionKindUpdate,
		Reason:          reconcile.ReasonContentChanged,
		Subject:         subject,
		Scope:           target.ScopeProject,
		Destination:     newDestination,
		DesiredHash:     newHash,
		LivePathHash:    oldHash,
		LiveFileMode:    0o700,
		DesiredFileMode: 0o700,
		PreviousState: &applyEventPreviousState{
			Subject:     subject,
			Scope:       target.ScopeProject,
			Destination: oldDestination,
			ContentHash: oldHash,
		},
	}
}

func (fixture *applyEventFixture) deleteAction(name string, destination string, oldContent string) applyEventAction {
	action := fixture.existingManagedAction(reconcile.ActionKindDelete, name, destination, oldContent)
	action.Reason = reconcile.ReasonRemovedFromManifest
	return action
}

func (fixture *applyEventFixture) recordAction(name string, destination string, content string) applyEventAction {
	subject := applyTestSubject(name)
	hash := artifact.HashFileContent([]byte(content))
	canonicalDestination := fixture.hookAssetDestination(name, hash)
	fixture.destinations[destination] = canonicalDestination
	fixture.writeExistingHostFile(canonicalDestination.String(), content)
	return applyEventAction{
		Kind:         reconcile.ActionKindRecord,
		Reason:       reconcile.ReasonManagedExisting,
		Subject:      subject,
		Scope:        target.ScopeProject,
		Destination:  canonicalDestination,
		DesiredHash:  hash,
		LivePathHash: hash,
		LiveFileMode: 0o600,
	}
}

func (fixture *applyEventFixture) existingManagedAction(kind reconcile.ActionKind, name string, destination string, content string) applyEventAction {
	subject := applyTestSubject(name)
	hash := artifact.HashFileContent([]byte(content))
	canonicalDestination := fixture.hookAssetDestination(name, hash)
	fixture.destinations[destination] = canonicalDestination
	fixture.writeExistingHostFile(canonicalDestination.String(), content)
	fixture.appendManagedPath(durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		canonicalDestination,
		hash,
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o600,
	))
	return applyEventAction{
		Kind:         kind,
		Subject:      subject,
		Scope:        target.ScopeProject,
		Destination:  canonicalDestination,
		LivePathHash: hash,
		LiveFileMode: 0o600,
		PreviousState: &applyEventPreviousState{
			Subject:     subject,
			Scope:       target.ScopeProject,
			Destination: canonicalDestination,
			ContentHash: hash,
		},
	}
}

func (fixture *applyEventFixture) appendManagedPath(state durable.ManagedPathState, err error) {
	if err != nil {
		panic(err)
	}
	paths := append(fixture.current.ManagedPaths(), state)
	fixture.current, err = fixture.current.WithManagedPaths(paths)
	if err != nil {
		panic(err)
	}
}

func (fixture *applyEventFixture) replacePayloadContent(subject topology.SubjectID, content string) {
	for index := range fixture.payloads {
		if fixture.payloads[index].Subject() == subject {
			file, ok := fixture.payloads[index].File()
			if !ok {
				panic("payload is not a file")
			}
			replacement, err := payload.NewFilePayload(subject, []byte(content), file.Mode())
			if err != nil {
				panic(err)
			}
			fixture.payloads[index] = replacement
			return
		}
	}
	panic("payload not found")
}

func (fixture *applyEventFixture) appendFilePayload(
	subject topology.SubjectID,
	content string,
	mode os.FileMode,
) {
	value, err := payload.NewFilePayload(subject, []byte(content), mode)
	if err != nil {
		panic(err)
	}
	fixture.payloads = append(fixture.payloads, value)
}

func applyTestSubject(name string) topology.SubjectID {
	id, err := entity.New(entity.KindHookAsset, name)
	if err != nil {
		panic(err)
	}
	subject, err := topologyprojection.Subject(id, topologyhook.AssetProjectProjectionNamespace)
	if err != nil {
		panic(err)
	}
	return subject
}

func (fixture *applyEventFixture) hookAssetDestination(name string, hash artifact.ContentHash) output.Destination {
	placement, err := profile.HookAssetPlacementFor(target.ScopeProject, []target.Target{target.TargetCodex})
	if err != nil {
		panic(err)
	}
	destination, err := placement.Destination(name, hash)
	if err != nil {
		panic(err)
	}
	return destination
}

func (fixture *applyEventFixture) writeExistingHostFile(destination string, content string) artifact.ContentHash {
	writeApplyEventFile(nil, fixture.hostPath(destination), content)
	return artifact.HashFileContent([]byte(content))
}

func (fixture *applyEventFixture) hostPath(destination string) string {
	if canonical, ok := fixture.destinations[destination]; ok {
		destination = canonical.String()
	}
	return filepath.Join(fixture.root, filepath.FromSlash(destination))
}

func assertEventKinds(t *testing.T, events []Event, want []EventKind) {
	t.Helper()

	got := make([]EventKind, 0, len(events))
	for _, event := range events {
		got = append(got, event.Kind)
	}
	if len(got) != len(want) {
		t.Fatalf("event kinds = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("event kinds = %#v, want %#v", got, want)
		}
	}
}

func assertActionEventIndexes(t *testing.T, events []Event, want []int) {
	t.Helper()

	var got []int
	for _, event := range events {
		if event.Action != nil {
			got = append(got, event.Action.Index)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("action indexes = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("action indexes = %#v, want %#v", got, want)
		}
	}
}

func assertAllEventsTotalActions(t *testing.T, events []Event, want int) {
	t.Helper()

	for _, event := range events {
		if event.TotalActions != want {
			t.Fatalf("event %#v TotalActions = %d, want %d", event.Kind, event.TotalActions, want)
		}
	}
}

func assertApplyEventNoErrors(t *testing.T, events []Event) {
	t.Helper()

	for _, event := range events {
		if event.Err != nil {
			t.Fatalf("event %#v Err = %v, want nil", event.Kind, event.Err)
		}
	}
}

func assertNoEventKind(t *testing.T, events []Event, reject EventKind) {
	t.Helper()

	for _, event := range events {
		if event.Kind == reject {
			t.Fatalf("events contain %q: %#v", reject, events)
		}
	}
}

func assertHostFileContent(t *testing.T, path string, want string) {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read host file %q: %v", path, err)
	}
	if string(content) != want {
		t.Fatalf("host file %q = %q, want %q", path, content, want)
	}
}

func assertHostMissing(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("host path %q exists or stat failed unexpectedly: %v", path, err)
	}
}

func writeApplyEventFile(t testing.TB, path string, content string) {
	if t != nil {
		t.Helper()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		if t == nil {
			panic(err)
		}
		t.Fatalf("create directory for %q: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		if t == nil {
			panic(err)
		}
		t.Fatalf("write file %q: %v", path, err)
	}
}
