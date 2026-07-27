package execute

import (
	"os"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/test/outputtest"
)

func TestManagedPathEffectRejectsMissingCurrentEvidence(t *testing.T) {
	previous := testManagedPathEffectState(t, "oracle", outputtest.Parse(t, ".agents/skills/oracle"))
	facts := managedPathEffectFacts{
		subject:          previous.Subject(),
		consumerTargets:  previous.ConsumerTargets(),
		scope:            previous.Scope(),
		destination:      previous.Destination(),
		desiredHash:      testArtifactHash("new"),
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
		previous:         &previous,
	}

	replace := ManagedPathEffect{replace: &managedPathReplaceEffect{facts: facts}}
	if err := replace.validate(); err == nil || !strings.Contains(err.Error(), "fresh live hash") {
		t.Fatalf("replace validation error = %v, want missing live evidence rejection", err)
	}
	record := ManagedPathEffect{record: &managedPathRecordEffect{facts: facts}}
	if err := record.validate(); err == nil || !strings.Contains(err.Error(), "fresh live hash") {
		t.Fatalf("record validation error = %v, want missing live evidence rejection", err)
	}
}

func TestManagedPathEffectAllowsRelocationWithAbsentNewDestination(t *testing.T) {
	previous := testManagedPathEffectState(t, "oracle", outputtest.Parse(t, ".agents/skills/oracle"))
	facts := managedPathEffectFacts{
		subject:          previous.Subject(),
		consumerTargets:  previous.ConsumerTargets(),
		scope:            previous.Scope(),
		destination:      outputtest.Parse(t, ".agents/skills-v2/oracle"),
		desiredHash:      testArtifactHash("new"),
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
		previous:         &previous,
	}

	effect := ManagedPathEffect{replace: &managedPathReplaceEffect{facts: facts}}
	if err := effect.validate(); err != nil {
		t.Fatalf("relocation effect validation: %v", err)
	}
}

func TestManagedPathEffectRejectsPreviousStateFromAnotherSubject(t *testing.T) {
	previous := testManagedPathEffectState(t, "review", outputtest.Parse(t, ".agents/skills/oracle"))
	oracle := testManagedPathEffectState(t, "oracle", outputtest.Parse(t, ".agents/skills/oracle"))
	facts := managedPathEffectFacts{
		subject:          oracle.Subject(),
		consumerTargets:  oracle.ConsumerTargets(),
		scope:            oracle.Scope(),
		destination:      oracle.Destination(),
		desiredHash:      testArtifactHash("new"),
		liveHash:         testArtifactHash("old"),
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
		previous:         &previous,
	}

	effect := ManagedPathEffect{replace: &managedPathReplaceEffect{facts: facts}}
	if err := effect.validate(); err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("cross-subject validation error = %v, want rejection", err)
	}
}

func TestManagedPathEffectRejectsMalformedDesiredAndLiveHashes(t *testing.T) {
	previous := testManagedPathEffectState(t, "oracle", outputtest.Parse(t, ".agents/skills/oracle"))
	facts := managedPathEffectFacts{
		subject:          previous.Subject(),
		consumerTargets:  previous.ConsumerTargets(),
		scope:            previous.Scope(),
		destination:      previous.Destination(),
		desiredHash:      testArtifactHash("new"),
		liveHash:         previous.ContentHash(),
		contentKind:      previous.ContentKind(),
		permissionPolicy: previous.PermissionPolicy(),
		previous:         &previous,
	}

	malformedDesired := facts
	malformedDesired.desiredHash = "sha256:short"
	if err := (ManagedPathEffect{
		replace: &managedPathReplaceEffect{facts: malformedDesired},
	}).validate(); err == nil || !strings.Contains(err.Error(), "desired hash") {
		t.Fatalf("malformed desired hash error = %v", err)
	}

	malformedLive := facts
	malformedLive.liveHash = artifact.ContentHash("sha512:" + strings.Repeat("a", 64))
	if err := (ManagedPathEffect{
		replace: &managedPathReplaceEffect{facts: malformedLive},
	}).validate(); err == nil || !strings.Contains(err.Error(), "replace effect live hash") {
		t.Fatalf("malformed live hash error = %v", err)
	}

	for _, test := range []struct {
		scope       target.Scope
		destination output.Destination
		want        string
	}{
		{scope: target.ScopeProject, destination: outputtest.Parse(t, "~/agents/skills/oracle"), want: "destination"},
		{scope: target.ScopeGlobal, destination: outputtest.Parse(t, ".agents/skills/oracle"), want: "destination"},
		{scope: target.ScopeProject, want: "destination"},
		{destination: outputtest.Parse(t, ".agents/skills/oracle"), want: "unknown scope"},
	} {
		malformedDestination := facts
		malformedDestination.scope = test.scope
		malformedDestination.destination = test.destination
		if err := (ManagedPathEffect{
			replace: &managedPathReplaceEffect{facts: malformedDestination},
		}).validate(); err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("scope %q destination %q error = %v", test.scope, test.destination, err)
		}
	}
}

func TestManagedPathEffectValidatesExactModeWithoutFamilyAssumptions(t *testing.T) {
	id, err := entity.New(entity.KindExtension, "future-exact")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, "future.project.exact")
	if err != nil {
		t.Fatal(err)
	}
	base := managedPathEffectFacts{
		subject: subject, consumerTargets: []target.Target{target.TargetCodex},
		scope: target.ScopeProject, destination: outputtest.Parse(t, ".daem/future-exact"),
		desiredHash: testArtifactHash("future"), contentKind: realization.PathProjectionFile,
	}
	for _, test := range []struct {
		name    string
		policy  realization.PathPermissionPolicy
		mode    os.FileMode
		wantErr string
	}{
		{name: "exact zero", policy: realization.PathPermissionsExact, mode: 0},
		{name: "exact 0640", policy: realization.PathPermissionsExact, mode: 0o640},
		{name: "exact 0750", policy: realization.PathPermissionsExact, mode: 0o750},
		{name: "exact type bits", policy: realization.PathPermissionsExact, mode: os.ModeSetuid | 0o700, wantErr: "permission bits only"},
		{name: "class noncanonical", policy: realization.PathPermissionsExecutableClass, mode: 0o640, wantErr: "canonical publish mode"},
	} {
		t.Run(test.name, func(t *testing.T) {
			facts := base
			facts.permissionPolicy = test.policy
			facts.desiredFileMode = test.mode
			err := (ManagedPathEffect{create: &managedPathCreateEffect{facts: facts}}).validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validate returned error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validate error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestManagedPathPayloadRejectsContentVariantAndExactModeMismatch(t *testing.T) {
	previous := testExactManagedPathEffectState(t)
	view, err := access.OpenView(t.TempDir())
	if err != nil {
		t.Fatalf("OpenView returned error: %v", err)
	}
	directoryHash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("View.Hash returned error: %v", err)
	}
	directoryIdentity, err := artifact.NewExactIdentity(
		"test:payload-variant",
		"",
		artifact.ArtifactKindDirectory,
		directoryHash,
	)
	if err != nil {
		t.Fatalf("NewExactIdentity returned error: %v", err)
	}
	directoryPayload, err := payload.NewDirectoryPayload(
		t.Context(),
		previous.Subject(),
		directoryIdentity,
		view,
	)
	if err != nil {
		t.Fatalf("NewDirectoryPayload returned error: %v", err)
	}
	filePayload, err := payload.NewFilePayload(previous.Subject(), []byte("file"), 0o600)
	if err != nil {
		t.Fatalf("NewFilePayload returned error: %v", err)
	}

	for _, test := range []struct {
		name    string
		facts   managedPathEffectFacts
		value   payload.Payload
		wantErr string
	}{
		{
			name: "directory payload for file effect",
			facts: managedPathEffectFacts{
				subject: previous.Subject(), destination: previous.Destination(), desiredHash: directoryHash,
				contentKind: realization.PathProjectionFile, permissionPolicy: realization.PathPermissionsExact,
				desiredFileMode: 0o600,
			},
			value:   directoryPayload,
			wantErr: "non-file payload",
		},
		{
			name: "file payload for directory effect",
			facts: managedPathEffectFacts{
				subject: previous.Subject(), destination: previous.Destination(), desiredHash: filePayload.Hash(),
				contentKind: realization.PathProjectionDirectory, permissionPolicy: realization.PathPermissionsNone,
			},
			value:   filePayload,
			wantErr: "non-directory payload",
		},
		{
			name: "file mode differs from exact effect",
			facts: managedPathEffectFacts{
				subject: previous.Subject(), destination: previous.Destination(), desiredHash: filePayload.Hash(),
				contentKind: realization.PathProjectionFile, permissionPolicy: realization.PathPermissionsExact,
				desiredFileMode: 0o640,
			},
			value:   filePayload,
			wantErr: "payload mode 0600 does not match planned mode 0640",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			set, err := payload.NewPayloadSet([]payload.Payload{test.value}, nil)
			if err != nil {
				t.Fatalf("NewPayloadSet returned error: %v", err)
			}
			effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: test.facts}}
			if _, err := managedPathPayload(effect, set); err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("managedPathPayload error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestManagedPathEffectRejectsUncorrelatedRecordAndRemoveEvidence(t *testing.T) {
	previous := testExactManagedPathEffectState(t)
	base := managedPathEffectFacts{
		subject: previous.Subject(), scope: previous.Scope(), destination: previous.Destination(),
		desiredHash: previous.ContentHash(), liveHash: previous.ContentHash(),
		contentKind: realization.PathProjectionFile, permissionPolicy: realization.PathPermissionsExact,
		desiredFileMode: 0o700, liveFileMode: 0o700,
	}

	recordHashMismatch := base
	recordHashMismatch.consumerTargets = previous.ConsumerTargets()
	recordHashMismatch.liveHash = testArtifactHash("foreign")
	if err := (ManagedPathEffect{record: &managedPathRecordEffect{facts: recordHashMismatch}}).validate(); err == nil || !strings.Contains(err.Error(), "live hash") {
		t.Fatalf("record hash mismatch error = %v", err)
	}

	recordModeMismatch := base
	recordModeMismatch.consumerTargets = previous.ConsumerTargets()
	recordModeMismatch.liveFileMode = 0o755
	if err := (ManagedPathEffect{record: &managedPathRecordEffect{facts: recordModeMismatch}}).validate(); err == nil || !strings.Contains(err.Error(), "live mode") {
		t.Fatalf("record mode mismatch error = %v", err)
	}

	removeHashMismatch := base
	removeHashMismatch.liveHash = testArtifactHash("foreign")
	removeHashMismatch.previous = &previous
	if err := (ManagedPathEffect{remove: &managedPathRemoveEffect{facts: removeHashMismatch}}).validate(); err == nil || !strings.Contains(err.Error(), "live hash") {
		t.Fatalf("remove hash mismatch error = %v", err)
	}

	removeModeMismatch := base
	removeModeMismatch.liveFileMode = 0o755
	removeModeMismatch.previous = &previous
	if err := (ManagedPathEffect{remove: &managedPathRemoveEffect{facts: removeModeMismatch}}).validate(); err == nil || !strings.Contains(err.Error(), "live mode") {
		t.Fatalf("remove mode mismatch error = %v", err)
	}
}

func testExactManagedPathEffectState(t *testing.T) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindHookAsset, "guard")
	if err != nil {
		t.Fatal(err)
	}
	placement, err := profile.HookAssetPlacementFor(target.ScopeProject, []target.Target{target.TargetCodex})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := topologyprojection.Subject(id, placement.ID())
	if err != nil {
		t.Fatal(err)
	}
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		outputtest.Parse(t, ".daem/hook-assets/guard/sha256-test/asset"),
		testArtifactHash("current"),
		realization.PathProjectionFile,
		realization.PathPermissionsExact,
		0o700,
	)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func testManagedPathEffectState(t *testing.T, name string, destination output.Destination) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindSkill, name)
	if err != nil {
		t.Fatalf("entity.New returned error: %v", err)
	}
	subject, err := topologyprojection.Subject(id, "skill.project.agents")
	if err != nil {
		t.Fatalf("projection.Subject returned error: %v", err)
	}
	state, err := durable.NewManagedPathState(
		subject,
		[]target.Target{target.TargetCodex},
		target.ScopeProject,
		destination,
		testArtifactHash("old"),
		realization.PathProjectionDirectory,
		realization.PathPermissionsNone,
		0,
	)
	if err != nil {
		t.Fatalf("NewManagedPathState returned error: %v", err)
	}
	return state
}
