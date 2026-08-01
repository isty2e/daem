//go:build darwin

package execute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/observe"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/payload"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/supply/artifact/access"
	"github.com/isty2e/daem/internal/target"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

func TestApplyNormalizationAliasesMutatesAtMostFirstAndRollsBack(t *testing.T) {
	fixture := newApplyEventFixture(t)
	composed := "Caf\u00e9"
	decomposed := "Cafe\u0301"
	probeRoot := filepath.Join(fixture.root, ".agents", "skills")
	if err := os.MkdirAll(probeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	probePath := filepath.Join(probeRoot, composed)
	if err := os.Mkdir(probePath, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(probeRoot, decomposed)); err != nil {
		_ = os.Remove(probePath)
		t.Skip("temporary filesystem does not resolve the tested normalization alias")
	}
	if err := os.Remove(probePath); err != nil {
		t.Fatal(err)
	}

	scenario := newPathAuthorityApplyScenario(t, []string{composed, decomposed})

	started := 0
	visibilityCollapse := errors.New("visibility identity collapse")
	_, err := ApplyWithOptions(t.Context(), scenario.input, scenario.options(func(event Event) {
		if event.Kind == EventActionStarted {
			started++
		}
	}, visibilityCollapse))
	if !errors.Is(err, visibilityCollapse) {
		t.Fatalf("ApplyWithOptions error = %v, want alias collapse rejection", err)
	}
	if started != 1 {
		t.Fatalf("started actions = %d, want only the first alias", started)
	}
	for _, destination := range scenario.destinations {
		if _, statErr := os.Lstat(scenario.fixture.hostPath(destination.String())); !os.IsNotExist(statErr) {
			t.Fatalf(
				"normalization alias %q stat = %v after apply error %v, want rolled back absence",
				destination,
				statErr,
				err,
			)
		}
	}
}

func TestApplyDistinctNormalizationSensitiveSiblingsCompleteSequentially(t *testing.T) {
	scenario := newPathAuthorityApplyScenario(t, []string{"Caf\u00e9", "Tea\u0301"})

	started := 0
	result, err := ApplyWithOptions(t.Context(), scenario.input, scenario.options(func(event Event) {
		if event.Kind == EventActionStarted {
			started++
		}
	}, errors.New("visibility identity collapse")))
	if err != nil {
		t.Fatalf("ApplyWithOptions returned error: %v", err)
	}
	if result.ActionCount != len(scenario.destinations) || started != len(scenario.destinations) {
		t.Fatalf(
			"apply result action count = %d, started = %d, want %d",
			result.ActionCount,
			started,
			len(scenario.destinations),
		)
	}
	for _, destination := range scenario.destinations {
		if info, statErr := os.Lstat(scenario.fixture.hostPath(destination.String())); statErr != nil {
			t.Fatalf("distinct sibling %q stat: %v", destination, statErr)
		} else if !info.IsDir() {
			t.Fatalf("distinct sibling %q mode = %v, want directory", destination, info.Mode())
		}
	}
}

type pathAuthorityApplyScenario struct {
	fixture      *applyEventFixture
	input        ApplyInput
	leases       *mutation.LeaseSet
	destinations []output.Destination
}

func newPathAuthorityApplyScenario(t *testing.T, spellings []string) pathAuthorityApplyScenario {
	t.Helper()

	fixture := newApplyEventFixture(t)
	placement, err := profile.ManagedPathPlacementForConsumers(
		entity.KindSkill,
		target.ScopeProject,
		"skill.project.agents",
		[]target.Target{target.TargetCodex},
	)
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceRoot, "SKILL.md"), []byte("same payload\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	view, err := access.OpenView(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := view.Hash(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := artifact.NewExactIdentity(
		"test:normalization-alias",
		"",
		artifact.ArtifactKindDirectory,
		hash,
	)
	if err != nil {
		t.Fatal(err)
	}

	effects := make([]ManagedPathEffect, 0, len(spellings))
	evidence := make([]observe.ManagedPathEvidence, 0, len(spellings))
	payloads := make([]payload.Payload, 0, len(spellings))
	domains := make([]mutation.Domain, 0, len(spellings)*2)
	destinations := make([]output.Destination, 0, len(spellings))
	for _, spelling := range spellings {
		entityID, err := entity.New(entity.KindSkill, spelling)
		if err != nil {
			t.Fatal(err)
		}
		subject, err := topologyprojection.Subject(entityID, placement.ID())
		if err != nil {
			t.Fatal(err)
		}
		destination, err := placement.ChildDestination(spelling)
		if err != nil {
			t.Fatal(err)
		}
		destinations = append(destinations, destination)
		effect := ManagedPathEffect{create: &managedPathCreateEffect{facts: managedPathEffectFacts{
			subject: subject, consumerTargets: []target.Target{target.TargetCodex},
			scope: target.ScopeProject, destination: destination, desiredHash: hash,
			contentKind: realization.PathProjectionDirectory, permissionPolicy: realization.PathPermissionsNone,
		}}}
		effects = append(effects, effect)
		observed, err := observe.NewManagedPathEvidence(subject, destination, false, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		evidence = append(evidence, observed)
		prepared, err := payload.NewDirectoryPayload(t.Context(), subject, identity, view)
		if err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, prepared)
		for _, effect := range []mutation.PathEffect{
			mutation.PathEffectDirectoryEntry,
			mutation.PathEffectReferent,
		} {
			domain, err := mutation.NewPhysicalPathDomain(mutation.PhysicalPathRequest{
				Path:   fixture.hostPath(destination.String()),
				Access: mutation.AccessExclusive,
				Effect: effect,
				Target: string(target.TargetCodex),
				Scope:  string(target.ScopeProject),
			})
			if err != nil {
				t.Fatal(err)
			}
			domains = append(domains, domain)
		}
	}
	store, err := mutation.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	leases, err := store.Acquire(t.Context(), domains...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = leases.Release() })
	payloadSet, err := payload.NewPayloadSet(payloads, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := ApplyInput{
		Paths: fixture.paths, Resolver: destinationResolver(fixture.paths),
		ManagedPathEffects: effects, ManagedPathEvidence: evidence,
		CurrentState: fixture.current, Payloads: payloadSet,
		StateCodec: testStateCodec(), Filesystem: testFilesystem(),
	}
	return pathAuthorityApplyScenario{
		fixture: fixture, input: input, leases: leases, destinations: destinations,
	}
}

func (scenario pathAuthorityApplyScenario) options(
	events func(Event),
	visibilityRejection error,
) ApplyOptions {
	return ApplyOptions{
		Events: events,
		ValidateBeforeEffects: func(ctx context.Context, authority mutation.PhysicalAuthoritySet) error {
			matches, err := scenario.leases.DomainsMatchCurrent(ctx)
			if err != nil || !matches {
				return errors.Join(errors.New("mutation domains changed"), err)
			}
			covered, err := scenario.leases.CoversPhysicalAuthority(authority)
			if err != nil || !covered {
				return errors.Join(errors.New("physical authority is not covered"), err)
			}
			return nil
		},
		AcceptVisibilityChanges: func(ctx context.Context) error {
			accepted, err := scenario.leases.AcceptVisibilityChanges(ctx)
			if err != nil || !accepted {
				return errors.Join(visibilityRejection, err)
			}
			return nil
		},
		ValidateCompensationAuthority: func(ctx context.Context) error {
			matches, err := scenario.leases.VisibilityAuthorityMatchesCurrent(ctx)
			if err != nil || !matches {
				return errors.Join(errors.New("compensation authority changed"), err)
			}
			return nil
		},
		AcceptCompensationVisibilityChanges: func(ctx context.Context) error {
			accepted, err := scenario.leases.AcceptVisibilityChanges(ctx)
			if err != nil || !accepted {
				return errors.Join(errors.New("compensation visibility rejected"), err)
			}
			return nil
		},
	}
}
