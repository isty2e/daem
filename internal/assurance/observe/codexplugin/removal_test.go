package codexplugin

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observepostcondition "github.com/isty2e/daem/internal/assurance/observe/postcondition"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	hostrelation "github.com/isty2e/daem/internal/realization/relation"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

const codexRemovalTestHash = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func TestObserveRemovalEffectsClassifiesExactCachePath(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)
	paths, err := ResolveHostPaths()
	if err != nil {
		t.Fatal(err)
	}
	pending := mustCodexPendingRemoval(t, codexHome, "documents@official")

	assertCodexRemovalEffectState(
		t,
		paths,
		pending,
		observepostcondition.EvidenceSatisfied,
	)
	cachePath, err := paths.PluginCachePath(pending.Identity().Carrier().Key())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cachePath, 0o700); err != nil {
		t.Fatal(err)
	}
	assertCodexRemovalEffectState(
		t,
		paths,
		pending,
		observepostcondition.EvidenceUnsatisfied,
	)
	if err := os.RemoveAll(cachePath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(codexHome, "missing-target"), cachePath); err != nil {
		t.Fatal(err)
	}
	assertCodexRemovalEffectState(
		t,
		paths,
		pending,
		observepostcondition.EvidenceUnsatisfied,
	)
}

func TestObserveCacheAbsenceFailsClosedOnFilesystemUncertainty(t *testing.T) {
	state := observeCacheAbsence(
		"/unreadable/cache",
		func(string) (fs.FileInfo, error) {
			return nil, os.ErrPermission
		},
	)
	if state != observepostcondition.EvidenceUnavailable {
		t.Fatalf("state = %q, want unavailable", state)
	}
	state = observeCacheAbsence(
		"/missing/cache",
		func(string) (fs.FileInfo, error) {
			return nil, fs.ErrNotExist
		},
	)
	if state != observepostcondition.EvidenceSatisfied {
		t.Fatalf("state = %q, want satisfied", state)
	}
	state = observeCacheAbsence(
		"/present/cache",
		func(string) (fs.FileInfo, error) {
			return nil, nil
		},
	)
	if state != observepostcondition.EvidenceUnsatisfied {
		t.Fatalf("state = %q, want unsatisfied", state)
	}
}

func TestCodexRemovalBaselineAndObservationValidateExactContract(t *testing.T) {
	root := t.TempDir()
	pending := mustCodexPendingRemoval(t, root, "documents@official")
	baselines, err := CaptureRemovalBaselines(
		context.Background(),
		pending.Identity().Carrier().Key(),
		pending.EffectPostconditions(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(baselines.Baselines()) != 0 {
		t.Fatalf("Codex cache-absence baselines = %#v, want none", baselines.Baselines())
	}
	if _, err := CaptureRemovalBaselines(
		context.Background(),
		pending.Identity().Carrier().Key(),
		effectpostcondition.Set{},
	); err == nil {
		t.Fatal("empty Codex removal postcondition was accepted")
	}
	if _, err := ObserveRemovalEffects(nil, HostPaths{}, pending); err == nil {
		t.Fatal("nil removal context was accepted")
	}
}

func assertCodexRemovalEffectState(
	t *testing.T,
	paths HostPaths,
	pending durablecarrier.PendingCarrierRemoval,
	want observepostcondition.EvidenceState,
) {
	t.Helper()
	evidence, err := ObserveRemovalEffects(context.Background(), paths, pending)
	if err != nil {
		t.Fatal(err)
	}
	facts := evidence.Evidence()
	if evidence.Subject() != pending.Identity().RelationSubject() ||
		!evidence.RouteRequest().Equal(pending.RemoveRequest()) ||
		len(facts) != 1 ||
		facts[0].Requirement() != effectpostcondition.CarrierArtifactsAbsent ||
		facts[0].State() != want {
		t.Fatalf("effect evidence = %#v / %#v, want %q", evidence, facts, want)
	}
}

func mustCodexPendingRemoval(
	t *testing.T,
	root string,
	selector string,
) durablecarrier.PendingCarrierRemoval {
	t.Helper()
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		selector,
	)
	if err != nil {
		t.Fatal(err)
	}
	carrierKey, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	value, err := desiredextension.New(desiredextension.Spec{
		Name:    "documents",
		Carrier: desiredextension.CarrierCodexPlugin,
		Target:  target.TargetCodex,
		Scope:   target.ScopeGlobal,
		Source:  source,
	})
	if err != nil {
		t.Fatal(err)
	}
	carrier, err := extensiontopology.NewCarrier(carrierKey)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := extensiontopology.Relation(value)
	if err != nil {
		t.Fatal(err)
	}
	subjectKey, err := hostrelation.NewSubjectKey(selector)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := hostrelation.Derive(carrierKey, subject, subjectKey)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := durablecarrier.NewManagedCarrierIdentity(carrier, subject, expected)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	installRequest := mustCodexRouteRequest(t, "codex.plugin-carrier.install")
	claim, err := durablecarrier.NewManagedCarrierClaim(
		owner,
		identity,
		installRequest,
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	requirements, err := effectpostcondition.NewSet(
		[]effectpostcondition.Requirement{effectpostcondition.CarrierArtifactsAbsent},
	)
	if err != nil {
		t.Fatal(err)
	}
	baselines, err := durablecarrier.NewEffectBaselineSet(nil)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierRemoval(
		claim,
		mustCodexRouteRequest(t, "codex.plugin-carrier.remove"),
		requirements,
		baselines,
	)
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func mustCodexRouteRequest(
	t *testing.T,
	routeID string,
) realizationdelegate.Request {
	t.Helper()
	request, err := realizationdelegate.NewRequest(routeID, "v1", codexRemovalTestHash)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
