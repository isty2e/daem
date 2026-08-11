//go:build darwin || linux

package execute

import (
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
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/test/outputtest"
)

func TestApplyRejectsDeepDirectoryRemovalCapacityBeforeJournalPublication(t *testing.T) {
	fixture := newApplyEventFixture(t)
	destination := outputtest.Parse(t, ".agents/skills/deep")
	projection := testManagedPathEffectState(t, "deep", destination)
	source := filepath.Join(t.TempDir(), "deep")
	current := source
	for index := 0; index <= recovery.MaximumArtifactTreeDepth; index++ {
		current = filepath.Join(current, "d")
	}
	if err := os.MkdirAll(current, 0o700); err != nil {
		t.Fatalf("create deep directory payload: %v", err)
	}
	view, err := access.OpenView(source)
	if err != nil {
		t.Fatalf("open deep directory payload: %v", err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatalf("hash deep directory payload: %v", err)
	}
	identity, err := artifact.NewExactIdentity(
		"test:forward-removal-depth",
		"",
		artifact.ArtifactKindDirectory,
		hash,
	)
	if err != nil {
		t.Fatalf("construct deep directory identity: %v", err)
	}
	prepared, err := payload.NewDirectoryPayload(t.Context(), projection.Subject(), identity, view)
	if err != nil {
		t.Fatalf("construct deep directory payload: %v", err)
	}
	payloads, err := payload.NewPayloadSet([]payload.Payload{prepared}, nil)
	if err != nil {
		t.Fatalf("construct deep directory payload set: %v", err)
	}
	effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
		subject:          projection.Subject(),
		consumerTargets:  projection.ConsumerTargets(),
		scope:            projection.Scope(),
		destination:      destination,
		desiredHash:      hash,
		contentKind:      realization.PathProjectionDirectory,
		permissionPolicy: realization.PathPermissionsNone,
	}}}
	evidence, err := observe.NewManagedPathEvidence(projection.Subject(), destination, false, "", 0)
	if err != nil {
		t.Fatalf("construct absent path evidence: %v", err)
	}

	result, err := ApplyWithOptions(t.Context(), ApplyInput{
		Paths:               fixture.paths,
		Resolver:            destinationResolver(fixture.paths),
		ManagedPathEffects:  []ManagedPathEffect{effect},
		ManagedPathEvidence: []observe.ManagedPathEvidence{evidence},
		CurrentState:        durable.EmptySnapshot(),
		Payloads:            payloads,
		StateCodec:          testStateCodec(),
		Filesystem:          testFilesystem(),
	}, ApplyOptions{})
	if err == nil || !strings.Contains(err.Error(), "maximum depth 64") {
		t.Fatalf("ApplyWithOptions error = %v, want bounded tree-depth rejection", err)
	}
	if result.ExecutionAttempted {
		t.Fatal("bounded removal rejection reported execution attempted")
	}
	if _, statErr := os.Stat(fixture.paths.RecoveryDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("recovery directory after preflight rejection = %v, want absent", statErr)
	}
	assertHostMissing(t, fixture.hostPath(destination.String()))
}
