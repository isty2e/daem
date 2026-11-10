package qualification_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"
	"github.com/isty2e/daem/internal/effect/journal/namespace/namespacetest"
	journalpublication "github.com/isty2e/daem/internal/effect/journal/publication"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"
	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

type exclusionRegistryReader struct {
	registry ownership.Registry
	err      error
}

func (reader exclusionRegistryReader) Load(ctx context.Context) (ownership.Registry, error) {
	if err := ctx.Err(); err != nil {
		return ownership.Registry{}, err
	}
	return reader.registry, reader.err
}

func (reader exclusionRegistryReader) LoadForClaimRemovals(ctx context.Context, _ []ownership.Claim, _ int, _ rootedpath.PhysicalTraversalBudget) (ownership.Registry, error) {
	return reader.Load(ctx)
}

func TestRestorationExclusionKeepsModeAndValueAuthoritySeparate(t *testing.T) {
	fixture := nativeJournalFixture(t)
	namespaces, err := journalqualification.LoadNamespaces(fixture.journal, t.Context(), fixture.read, fixture.roots)
	if err != nil || len(namespaces) != 1 {
		t.Fatal(err)
	}
	observed := namespaces[0].Observation()
	foreign := documentClaimsForTest(t)
	foreign.exact = fixture.claims.exact
	mode := recovery.PermissionMode(0o600)
	current := fixture.comparison.ExpectedDocument()
	value, err := fixture.comparison.RestoreBefore(t.Context(), current, &mode)
	if err != nil {
		t.Fatal(err)
	}
	otherMode := recovery.PermissionMode(0o640)
	metadata, err := journaldocument.NewRestoration(current, &otherMode, current, &mode)
	if err != nil {
		t.Fatal(err)
	}
	unchanged, err := journaldocument.NewRestoration(current, &mode, current, &mode)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name        string
		restoration journaldocument.Restoration
		footprint   output.Footprint
		allowed     bool
	}{
		{"value/overlap", value, documentKeyFootprint(t, "model"), false},
		{"value/disjoint", value, documentKeyFootprint(t, "foreign"), true},
		{"mode/disjoint-key", metadata, documentKeyFootprint(t, "foreign"), true},
		{"mode/whole", metadata, output.WholePathFootprint(), false},
		{"no-op/whole", unchanged, output.WholePathFootprint(), true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			registry, err := ownership.NewRegistry([]ownership.Claim{documentActiveClaim(t, foreign, scenario.footprint)})
			if err != nil {
				t.Fatal(err)
			}
			err = journalqualification.RequireRestorationWriteExclusion(fixture.claims.owner, registry, observed, scenario.restoration)
			var conflict *ownership.ForeignClaimError
			if scenario.allowed && err != nil || !scenario.allowed && !errors.As(err, &conflict) {
				t.Fatal("restoration confused value, mode or no-host effects", err)
			}
		})
	}
}

func TestRestorationExclusionRequiresQualifiedChangeButNotNoOp(t *testing.T) {
	owner := documentClaimsForTest(t).owner
	mode := recovery.PermissionMode(0o600)
	current := aggregate.ExistingDocument([]byte(`{"a":0}`))
	updated := aggregate.ExistingDocument([]byte(`{"a":1}`))
	for _, scenario := range []struct {
		candidate aggregate.Document
		allowed   bool
	}{{current, true}, {updated, false}} {
		restored, err := journaldocument.NewRestoration(current, &mode, scenario.candidate, &mode)
		if err != nil {
			t.Fatal(err)
		}
		err = journalqualification.RequireRestorationWriteExclusion(owner, ownership.Registry{}, mutation.DirectoryEntryAuthorityObservation{}, restored)
		if scenario.allowed != (err == nil) {
			t.Fatal("no-op gained a new observation dependency or a write lost its basis", err)
		}
	}
}

func TestRestorationExclusionUsesMissingPathAncestorAuthority(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	budget := nativeFixtureBudget(t)
	observed, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(root, "missing", "Caf\u00e9.json"), physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	ancestor, err := mutation.ObserveDirectoryEntryAuthorityBounded(root, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	foreign := documentClaimsForTest(t)
	foreign.exact, _ = ancestor.Exact()
	registry, err := ownership.NewRegistry([]ownership.Claim{documentActiveClaim(t, foreign, output.WholePathFootprint())})
	if err != nil {
		t.Fatal(err)
	}
	mode := recovery.PermissionMode(0o600)
	restored, err := journaldocument.NewRestoration(aggregate.AbsentDocument(), nil, aggregate.ExistingDocument([]byte(`{"a":0}`)), &mode)
	if err != nil {
		t.Fatal(err)
	}
	owner := documentClaimsForTest(t).owner
	var conflict *ownership.ForeignClaimError
	if err := journalqualification.RequireRestorationWriteExclusion(owner, registry, observed, restored); !errors.As(err, &conflict) {
		t.Fatal("missing path lost its foreign physical-ancestor exclusion", err)
	}
	if err := journalqualification.RequireRestorationWriteExclusion(owner, ownership.EmptyRegistry(), observed, restored); err != nil {
		t.Fatal("unclaimed independently observed missing path was refused", err)
	}
}

func TestNative16ProjectRollbackRequiresFreshForeignExclusion(t *testing.T) {
	for _, overlap := range []bool{true, false} {
		t.Run(map[bool]string{true: "overlap", false: "disjoint"}[overlap], func(t *testing.T) {
			fixture, _ := publishedPiOrderFixture(t, target.ScopeProject)
			after, err := fixture.before.AdvanceNativeDocumentCommit()
			if err != nil {
				t.Fatal(err)
			}
			qualified, err := fixture.roots.Qualify(t.Context(), fixture.read.Budget)
			if err != nil {
				t.Fatal(err)
			}
			manifest, present := qualified.Manifest()
			if !present {
				t.Fatal("native16 fixture has no manifest root")
			}
			journal, err := journalrecord.NewNativeCapture("native-project-exclusion", time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), fixture.claims.owner, manifest.Provenance(), fixture.before, after, fixture.capturedGroups)
			if err != nil {
				t.Fatal(err)
			}
			journal, artifacts := publishNativeFixture(t, journal, fixture.comparison, journalpublication.StageOptions{StateCodec: fixture.codec, Codecs: fixture.read.Codecs, Limits: fixture.read.Limits, Budget: fixture.read.Budget})
			fixture.read.Artifacts = artifacts
			transitions, err := journal.ClaimTransitions()
			if err != nil || len(transitions) != 0 {
				t.Fatal("project control contains recorded ordinary ownership work", err)
			}
			loaded, err := journalqualification.LoadDocuments(journal, t.Context(), fixture.read)
			if err != nil {
				t.Fatal(err)
			}
			claims := requireLoadedNativeDocument(t, loaded, 0).Claims()
			if len(claims.Before())+len(claims.After()) != 0 {
				t.Fatal("native16 project control invented ownership history")
			}
			path := filepath.Join(namespacetest.LegacyRoot(t, fixture.roots, output.RootProject).SelectedPath, ".pi", "settings.json")
			expected := fixture.comparison.ExpectedDocument().Content()
			if err := os.WriteFile(path, expected, 0o640); err != nil {
				t.Fatal(err)
			}
			foreign := documentClaimsForTest(t)
			foreign.exact = fixture.claims.exact
			key := "foreign"
			if overlap {
				key = "packages"
			}
			registry, err := ownership.NewRegistry([]ownership.Claim{documentActiveClaim(t, foreign, documentKeyFootprint(t, key))})
			if err != nil {
				t.Fatal(err)
			}
			options := journalqualification.NativeReadOptions{
				Documents: fixture.read, Roots: fixture.roots, Registry: exclusionRegistryReader{registry: registry},
				State: func(context.Context) (durable.Snapshot, error) { return fixture.before, nil },
			}
			assessment, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			var conflict *ownership.ForeignClaimError
			if overlap && !errors.As(err, &conflict) || !overlap && (err != nil || assessment.Facts().Classification() != recovery.ClassificationNeedsRollback) {
				t.Fatal("native16 project restoration did not use the fresh foreign envelope", err)
			}
			options.State = func(context.Context) (durable.Snapshot, error) { return after, nil }
			committed, err := journalqualification.AssessNativeDocuments(journal, t.Context(), options)
			if err != nil || committed.Facts().Classification() != recovery.ClassificationCleanAfter {
				t.Fatal("committed no-host cleanup acquired a foreign-only refusal", err)
			}
			if actual, err := os.ReadFile(path); err != nil || string(actual) != string(expected) {
				t.Fatal("ownership assessment changed the host file", err)
			}
		})
	}
}
