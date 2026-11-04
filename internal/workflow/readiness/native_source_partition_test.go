package readiness

import (
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	desiredskill "github.com/isty2e/daem/internal/desired/skill"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
)

func nativePreparationEnvironment(t *testing.T, configs ...hostconfig.Config) desired.Environment {
	t.Helper()
	defaults, err := desired.NewDefaults(target.ScopeProject, desiredskill.InstallModeCopy)
	if err != nil {
		t.Fatal(err)
	}
	environment, err := desired.New(desired.Spec{Targets: []target.Target{target.TargetPi, target.TargetCodex}, Defaults: defaults, HostConfigs: configs})
	if err != nil {
		t.Fatal(err)
	}
	return environment
}

func TestNativeSourcePartitionPreservesActualAbsenceAndOtherIntent(t *testing.T) {
	for _, variant := range []string{"declaration removed", "whole mode", "other declaration", "unselected", "retargeted"} {
		t.Run(variant, func(t *testing.T) {
			fixture := newNativePreparationFixture(t, target.ScopeProject, `{}`)
			config := fixture.config
			name, path, mode := "settings", config.Path(), config.Ownership()
			switch variant {
			case "whole mode":
				mode = hostconfig.OwnershipDocument
			case "other declaration":
				name = "another"
			case "retargeted":
				path = "extension-settings/elsewhere.json"
			}
			changed, err := hostconfig.New(hostconfig.Spec{Name: name, Source: config.Source(), Target: config.Target(), Scope: config.Scope(), Path: path, Ownership: mode, Syntax: config.Syntax()})
			if err != nil {
				t.Fatal(err)
			}
			fixture.input.Environment = nativePreparationEnvironment(t, changed)
			if variant == "declaration removed" {
				fixture.input.Environment = nativePreparationEnvironment(t)
			}
			if variant == "unselected" {
				fixture.input.Selection, err = targetselection.ForDiagnostics([]string{"codex"})
				if err != nil {
					t.Fatal(err)
				}
			}
			fixture.refreshEpoch(t)
			prepared, err := PrepareNativeSources(t.Context(), fixture.input)
			if variant == "retargeted" {
				if err == nil || prepared.Validate() == nil || len(prepared.RemainingManagedPaths()) != 0 {
					t.Fatal("a different placement reused the old whole baseline", err)
				}
				return
			}
			if err != nil || prepared.Validate() != nil || len(prepared.Narrowings()) != 0 || len(prepared.RemainingManagedPaths()) != 1 || !prepared.RemainingManagedPaths()[0].Equal(fixture.observation.Before.ManagedPaths()[0]) {
				t.Fatal("absence, whole intent or a different declaration consumed prior ownership", err)
			}
			remaining := prepared.RemainingManagedPaths()
			remaining[0] = durable.ManagedPathState{}
			if !prepared.RemainingManagedPaths()[0].Equal(fixture.observation.Before.ManagedPaths()[0]) {
				t.Fatal("returned remaining rows mutated the partition")
			}
			wantSources := 1
			if variant == "declaration removed" || variant == "unselected" {
				wantSources = 0
			}
			if len(prepared.Sources()) != wantSources {
				t.Fatal("selected source coverage changed")
			}
		})
	}
}

func TestNativeSourcePartitionDiscardsEarlierValidSourceOnLaterFailure(t *testing.T) {
	fixture := newNativePreparationFixture(t, target.ScopeProject, `{}`)
	secondPath := filepath.Join(filepath.Dir(fixture.sourcePath), "second.json")
	requireNativePreparationFile(t, secondPath, []byte(`{"second":true}`))
	secondSource, err := source.NewLocalSource(secondPath, source.LocalSourceModeVendor)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hostconfig.New(hostconfig.Spec{Name: "z-second", Source: secondSource, Target: target.TargetPi, Scope: target.ScopeProject, Path: "extension-settings/second.json", Ownership: hostconfig.OwnershipKeys, Syntax: hostconfig.SyntaxJSON})
	if err != nil {
		t.Fatal(err)
	}
	fixture.input.Environment = nativePreparationEnvironment(t, fixture.config, second)
	fixture.refreshEpoch(t)
	if first, err := fixture.input.SourceEpoch.NativeSource(t.Context(), fixture.config); err != nil || first.Validate() != nil {
		t.Fatal("first source must actually remain usable", err)
	}
	requireNativePreparationFile(t, secondPath, []byte(`{"changed":true}`))
	prepared, err := PrepareNativeSources(t.Context(), fixture.input)
	if err == nil || prepared.Validate() == nil || len(prepared.Sources()) != 0 || len(prepared.Narrowings()) != 0 || len(prepared.RemainingManagedPaths()) != 0 {
		t.Fatal("late source failure returned a usable earlier prefix", err)
	}
}
