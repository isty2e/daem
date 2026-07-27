package lock

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/entity"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/realization/aggregate/codec/hook"
	lockmodel "github.com/isty2e/daem/internal/realization/lock"
	lockbuild "github.com/isty2e/daem/internal/realization/lock/build"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/acquisition"
	sourceresolution "github.com/isty2e/daem/internal/supply/source/resolution"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func TestSourceEpochBatchesInLegacyErrorOrderAndObservesCanonicalOrder(t *testing.T) {
	fixture := newMixedSourceEpochFixture(t)
	recorder := &recordingSourceBatchResolver{inner: fixture.resolver}

	epoch, err := resolveSourceEpochWithResolver(
		context.Background(),
		recorder,
		fixture.environment,
		fixture.locked,
		fixture.selection,
	)
	if err != nil {
		t.Fatalf("resolveSourceEpochWithResolver returned error: %v", err)
	}
	wantRequests := []acquisition.RequestID{
		"lock-observe:instructions:project",
		"lock-observe:skill:oracle",
		"lock-observe:hook_asset:guard",
	}
	if len(recorder.requests) != len(wantRequests) {
		t.Fatalf("batch requests = %d, want %d", len(recorder.requests), len(wantRequests))
	}
	for index, want := range wantRequests {
		if got := recorder.requests[index].ID(); got != want {
			t.Fatalf("batch request[%d] id = %q, want %q", index, got, want)
		}
	}

	observations, err := epoch.Observations(context.Background())
	if err != nil {
		t.Fatalf("SourceEpoch.Observations returned error: %v", err)
	}
	supplies := observations.ExactSupplies()
	wantSubjects := []lockmodel.LockedSubjectContract{
		mustExactSupplySubject(t, fixture.locked, fixture.environment.Skills()[0].ID()),
		mustExactSupplySubject(t, fixture.locked, fixture.environment.Instructions()[0].ID()),
		mustExactSupplySubject(t, fixture.locked, fixture.environment.HookAssets()[0].ID()),
	}
	if len(supplies) != len(wantSubjects) {
		t.Fatalf("exact Supply observations = %d, want %d", len(supplies), len(wantSubjects))
	}
	for index, want := range wantSubjects {
		if got := supplies[index].Subject(); got != want.SubjectID() {
			t.Fatalf("observation[%d] subject = %q, want %q", index, got, want.SubjectID())
		}
	}
}

func TestSourceEpochRetainsSuccessfulSiblingFactWhenAnotherRequestFails(t *testing.T) {
	fixture := newMixedSourceEpochFixture(t)
	resolver := &failingSourceBatchResolver{
		inner:  fixture.resolver,
		failID: "lock-observe:instructions:project",
	}

	epoch, err := resolveSourceEpochWithResolver(
		context.Background(),
		resolver,
		fixture.environment,
		fixture.locked,
		fixture.selection,
	)
	if err != nil {
		t.Fatalf("resolveSourceEpochWithResolver returned top-level error: %v", err)
	}
	if _, ok := epoch.SkillResolution(fixture.environment.Skills()[0], fixture.selection); !ok {
		t.Fatal("successful skill sibling resolution is unavailable")
	}
	if _, err := epoch.Observations(context.Background()); err == nil ||
		!strings.Contains(err.Error(), `instructions "project": injected source failure`) {
		t.Fatalf("SourceEpoch.Observations error = %v, want entity-specific instruction failure", err)
	}
}

func TestSourceEpochPropagatesTopLevelCancellation(t *testing.T) {
	fixture := newMixedSourceEpochFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := resolveSourceEpochWithResolver(
		ctx,
		fixture.resolver,
		fixture.environment,
		fixture.locked,
		fixture.selection,
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("resolveSourceEpochWithResolver error = %v, want context.Canceled", err)
	}
}

func TestSourceEpochRejectsSkillSourceAndSelectionDrift(t *testing.T) {
	fixture := newMixedSourceEpochFixture(t)
	epoch, err := resolveSourceEpochWithResolver(
		context.Background(),
		fixture.resolver,
		fixture.environment,
		fixture.locked,
		fixture.selection,
	)
	if err != nil {
		t.Fatalf("resolveSourceEpochWithResolver returned error: %v", err)
	}
	resource := fixture.environment.Skills()[0]
	otherSource, err := source.NewLocalSource("skills/other", source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := desiredskill.New(desiredskill.Spec{
		Name:         resource.ID().Name(),
		InstallName:  resource.InstallName(),
		Source:       otherSource,
		Targets:      resource.Targets(),
		Scope:        resource.Scope(),
		InstallMode:  resource.InstallMode(),
		Portable:     resource.Portable(),
		CompatRepair: resource.CompatRepair(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := epoch.SkillResolution(changed, fixture.selection); ok {
		t.Fatal("source-drifted skill reused an epoch resolution")
	}
	otherSelection, err := targetselection.ForDiagnostics([]string{string(target.TargetClaudeCode)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := epoch.SkillResolution(resource, otherSelection); ok {
		t.Fatal("selection-drifted skill reused an epoch resolution")
	}
}

type mixedSourceEpochFixture struct {
	resolver    acquisition.BatchResolver
	environment desired.Environment
	locked      lockmodel.File
	selection   targetselection.Selection
}

func newMixedSourceEpochFixture(t *testing.T) mixedSourceEpochFixture {
	t.Helper()

	root := t.TempDir()
	manifestPath := filepath.Join(root, "daem.toml")
	writeTestFile(t, root, "instructions/project.md", "project instructions\n")
	writeTestFile(t, root, "skills/oracle/SKILL.md", "---\nname: oracle\ndescription: oracle\n---\n")
	writeTestFile(t, root, "hooks/guard.sh", "#!/bin/sh\nexit 0\n")
	writeTestFile(t, root, "daem.toml", `
version = 1
targets = ["codex"]

[instructions.project]
source = "instructions/project.md"
targets = ["codex"]

[[skill]]
name = "oracle"
source = { path = "skills/oracle", mode = "vendor" }
targets = ["codex"]

[hook_asset.guard]
source = "hooks/guard.sh"
kind = "file"
scope = "project"
executable = true

[[hook]]
name = "guard"
event = "PreToolUse"
command = "{hook_file:guard} --check"
targets = ["codex"]
scope = "project"
`)
	environment := parseTestManifest(t, string(mustReadTestFile(t, manifestPath)))
	selection := testSelection(t, "codex")
	paths := resolveTestPaths(t, manifestPath)
	resolver, err := sourceresolution.NewResolver(paths)
	if err != nil {
		t.Fatalf("NewResolver returned error: %v", err)
	}
	locked, err := lockbuild.BuildWithOptions(context.Background(), environment, resolver, lockbuild.Options{
		HookContributionEncoder: hookcodec.CanonicalHookContribution,
	})
	if err != nil {
		t.Fatalf("lockbuild.BuildWithOptions returned error: %v", err)
	}
	return mixedSourceEpochFixture{
		resolver:    resolver,
		environment: environment,
		locked:      locked,
		selection:   selection,
	}
}

type recordingSourceBatchResolver struct {
	inner    acquisition.BatchResolver
	requests []acquisition.Request
}

func (resolver *recordingSourceBatchResolver) ResolveBatch(
	ctx context.Context,
	requests []acquisition.Request,
	options acquisition.BatchOptions,
) ([]acquisition.Result, error) {
	resolver.requests = append([]acquisition.Request(nil), requests...)
	return resolver.inner.ResolveBatch(ctx, requests, options)
}

type failingSourceBatchResolver struct {
	inner  acquisition.BatchResolver
	failID acquisition.RequestID
}

func (resolver *failingSourceBatchResolver) ResolveBatch(
	ctx context.Context,
	requests []acquisition.Request,
	options acquisition.BatchOptions,
) ([]acquisition.Result, error) {
	results, err := resolver.inner.ResolveBatch(ctx, requests, options)
	if err != nil {
		return nil, err
	}
	for index, request := range requests {
		if request.ID() != resolver.failID {
			continue
		}
		results[index], err = acquisition.NewFailureResult(request, fmt.Errorf("injected source failure"))
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}

func mustExactSupplySubject(
	t *testing.T,
	locked lockmodel.File,
	id entity.ID,
) lockmodel.LockedSubjectContract {
	t.Helper()

	contract, ok := locked.Locked.ExactSupplySubject(id)
	if !ok {
		t.Fatalf("locked exact Supply subject for %s %q is missing", id.Kind(), id.Name())
	}
	return contract
}
