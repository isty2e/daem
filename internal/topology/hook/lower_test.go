package hook

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	desiredhook "github.com/isty2e/daem/internal/desired/hook"
	desiredhookasset "github.com/isty2e/daem/internal/desired/hookasset"
	desiredfixture "github.com/isty2e/daem/internal/desired/testfixture"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/supply/source/sourcetest"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func TestLowerOwnsStableHookAssetConsumptionTopology(t *testing.T) {
	assets := []desiredhookasset.HookAsset{topologyHookAsset(t, "guard", target.ScopeProject)}
	hooks := []desiredhook.Hook{
		topologyHook(t, "codex-guard", target.ScopeProject, []target.Target{target.TargetCodex}, "run {hook_file:guard}"),
		topologyHook(t, "claude-guard", target.ScopeProject, []target.Target{target.TargetClaudeCode}, "run {hook_file:guard}"),
		topologyHook(t, "command-only", target.ScopeProject, []target.Target{target.TargetCodex}, "echo ok"),
	}
	model, err := Lower(assets, hooks)
	if err != nil {
		t.Fatalf("Lower returned error: %v", err)
	}
	if len(model.Projections()) != 3 || len(model.AssetProjections()) != 1 {
		t.Fatalf("lowered projections = %d Hook, %d asset", len(model.Projections()), len(model.AssetProjections()))
	}
	asset := model.AssetProjections()[0]
	if asset.EntityID() != assets[0].ID() || asset.Scope() != target.ScopeProject ||
		asset.SubjectID().Namespace() != AssetProjectProjectionNamespace {
		t.Fatalf("asset projection = %#v", asset)
	}
	consumers := model.consumerSubjectsOf(asset.SubjectID())
	if len(consumers) != 2 {
		t.Fatalf("asset consumers = %v, want two referenced Hook subjects", consumers)
	}
	if got := model.ConsumerTargetsOf(asset.SubjectID()); !slices.Equal(got, []target.Target{target.TargetClaudeCode, target.TargetCodex}) {
		t.Fatalf("asset consumer targets = %v", got)
	}
	for _, consumer := range consumers {
		if got := model.AssetSubjectsOf(consumer); !slices.Equal(got, []topology.SubjectID{asset.SubjectID()}) {
			t.Fatalf("Hook %q consumed subjects = %v", consumer, got)
		}
	}
	for _, projection := range model.Projections() {
		if projection.EntityID().Name() == "command-only" && len(model.AssetSubjectsOf(projection.SubjectID())) != 0 {
			t.Fatalf("command-only Hook %q acquired an asset relation", projection.SubjectID())
		}
	}
}

func TestLowerOwnsTheCompleteDesiredTopology(t *testing.T) {
	assets := []desiredhookasset.HookAsset{topologyHookAsset(t, "guard", target.ScopeProject)}
	hooks := []desiredhook.Hook{
		topologyHook(t, "shared", target.ScopeProject, []target.Target{
			target.TargetCodex,
			target.TargetClaudeCode,
		}, "run {hook_file:guard}"),
	}

	model, err := Lower(assets, hooks)
	if err != nil {
		t.Fatalf("Lower returned error: %v", err)
	}
	if len(model.Projections()) != 2 {
		t.Fatalf("declared projections = %#v, want both targets", model.Projections())
	}
	if got := model.ConsumerTargetsOf(model.AssetProjections()[0].SubjectID()); !slices.Equal(
		got,
		[]target.Target{target.TargetClaudeCode, target.TargetCodex},
	) {
		t.Fatalf("consumer targets = %v, want complete declared target set", got)
	}
}

func TestLowerRejectsInvalidCrossSubjectRelations(t *testing.T) {
	projectAsset := topologyHookAsset(t, "guard", target.ScopeProject)
	baseHook := topologyHook(t, "guard", target.ScopeProject, []target.Target{target.TargetCodex}, "run {hook_file:guard}")
	for _, test := range []struct {
		name    string
		assets  []desiredhookasset.HookAsset
		hooks   []desiredhook.Hook
		wantErr string
	}{
		{name: "unknown asset", hooks: []desiredhook.Hook{baseHook}, wantErr: `hook asset "guard" is not declared`},
		{
			name: "wrong scope", assets: []desiredhookasset.HookAsset{topologyHookAsset(t, "guard", target.ScopeGlobal)},
			hooks: []desiredhook.Hook{baseHook}, wantErr: `scope "global" does not match hook scope "project"`,
		},
		{
			name: "unsupported selected target", assets: []desiredhookasset.HookAsset{projectAsset},
			hooks:   []desiredhook.Hook{topologyHook(t, "guard", target.ScopeProject, []target.Target{target.TargetOpenCode}, "run {hook_file:guard}")},
			wantErr: "require a supported Codex or Claude Code",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Lower(test.assets, test.hooks)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("Lower error = %v, want containing %q", err, test.wantErr)
			}
		})
	}
}

func TestAssetSubjectIdentityExcludesContentAndConsumers(t *testing.T) {
	asset := topologyHookAsset(t, "guard", target.ScopeProject)
	first, err := AssetSubjectID(asset.ID(), asset.Scope())
	if err != nil {
		t.Fatal(err)
	}
	second, err := AssetSubjectID(asset.ID(), asset.Scope())
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first.Key() != asset.ID().String() || first.Namespace() != AssetProjectProjectionNamespace {
		t.Fatalf("asset subjects = %q and %q", first, second)
	}
	if strings.Contains(first.String(), "sha256") || strings.Contains(first.String(), "codex") {
		t.Fatalf("asset subject leaked content or consumer facts: %q", first)
	}
}

func topologyHookAsset(t *testing.T, name string, scope target.Scope) desiredhookasset.HookAsset {
	t.Helper()
	sourcePath := "hooks/" + name + ".sh"
	if scope == target.ScopeGlobal {
		sourcePath = filepath.Join(t.TempDir(), name+".sh")
	}
	return desiredfixture.HookAsset(t, desiredhookasset.Spec{
		Name: name, Source: sourcetest.Local(t, sourcePath, source.LocalSourceModeVendor),
		ArtifactKind: desiredhookasset.ArtifactKindFile, Scope: scope, Executable: true,
	})
}

func topologyHook(
	t *testing.T,
	name string,
	scope target.Scope,
	targets []target.Target,
	command string,
) desiredhook.Hook {
	t.Helper()
	return desiredfixture.Hook(t, desiredhook.Spec{
		Name: name, Event: "Stop", Type: desiredhook.TypeCommand, Command: command,
		Targets: targets, Scope: scope,
	})
}
