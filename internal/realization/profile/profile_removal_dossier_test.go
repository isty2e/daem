package profile

import (
	"slices"
	"strings"
	"testing"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/realization/effectpostcondition"
	"github.com/isty2e/daem/internal/target"
)

func TestClaudeRemovalDossierFreezesScopedRelationOnlyEnvelope(t *testing.T) {
	routeProfile, ok := Profile(target.TargetClaudeCode).DelegatedRoute(
		desiredextension.CarrierClaudeCodePlugin,
	)
	if !ok {
		t.Fatal("Claude delegated route profile is missing")
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"context7@official",
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierClaudeCodePlugin,
				target.TargetClaudeCode,
				scope,
				source,
			)
			if err != nil {
				t.Fatal(err)
			}
			dossier, admitted, err := routeProfile.RemovalDossier(key)
			if err != nil || !admitted {
				t.Fatalf("RemovalDossier = %#v, %t, %v", dossier, admitted, err)
			}
			if dossier.RequiresExistingTrust() ||
				dossier.PreservesSharedCarrier() ||
				len(dossier.EffectPostconditions().Requirements()) != 0 ||
				!slices.Contains(dossier.RemovedEffects(), "selected_claude_plugin_relation") ||
				!slices.Contains(dossier.RemovedEffects(), "selected_effective_bundled_exposure") ||
				!slices.Contains(dossier.RetainedEffects(), "plugin_persistent_data") ||
				!slices.Contains(dossier.RetainedEffects(), "orphaned_version_cache") ||
				!slices.Contains(dossier.NonClaims(), "bundled_contribution_individual_cleanup") {
				t.Fatalf("Claude removal dossier = %#v", dossier)
			}
		})
	}
}

func TestCodexRemovalDossierFreezesGlobalCoupledEffectEnvelope(t *testing.T) {
	routeProfile, ok := Profile(target.TargetCodex).DelegatedRoute(
		desiredextension.CarrierCodexPlugin,
	)
	if !ok {
		t.Fatal("Codex delegated route profile is missing")
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		"context7@official",
	)
	if err != nil {
		t.Fatal(err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		t.Fatal(err)
	}
	dossier, admitted, err := routeProfile.RemovalDossier(key)
	if err != nil || !admitted {
		t.Fatalf("RemovalDossier = %#v, %t, %v", dossier, admitted, err)
	}
	requirements := dossier.EffectPostconditions().Requirements()
	if dossier.RequiresExistingTrust() ||
		dossier.PreservesSharedCarrier() ||
		len(requirements) != 1 ||
		requirements[0] != effectpostcondition.CarrierArtifactsAbsent ||
		!slices.Contains(dossier.RemovedEffects(), "selected_codex_plugin_relation") ||
		!slices.Contains(dossier.RemovedEffects(), "selected_versioned_bundle_cache") ||
		!slices.Contains(dossier.RetainedEffects(), "marketplace_snapshot") ||
		!slices.Contains(dossier.RetainedEffects(), "sibling_plugin_relations_and_caches") ||
		!slices.Contains(dossier.NonClaims(), "marketplace_removal_or_prune") {
		t.Fatalf("Codex removal dossier = %#v", dossier)
	}
}

func TestPiRemovalDossierSeparatesSourceEffectsAndScopeTrust(t *testing.T) {
	routeProfile, ok := Profile(target.TargetPi).DelegatedRoute(desiredextension.CarrierPiPackage)
	if !ok {
		t.Fatal("Pi delegated route profile is missing")
	}
	tests := []struct {
		name              string
		scope             target.Scope
		source            string
		wantTrust         bool
		wantPostcondition effectpostcondition.Requirement
		wantRemoved       string
		wantRetained      string
		wantNonClaim      string
	}{
		{
			name:              "project npm",
			scope:             target.ScopeProject,
			source:            "npm:@acme/tools@1.2.3",
			wantTrust:         true,
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantRemoved:       "selected_scoped_npm_package_artifacts",
			wantRetained:      "npm_cache_and_logs",
			wantNonClaim:      "dependency_gc_beyond_native_remove",
		},
		{
			name:              "global git",
			scope:             target.ScopeGlobal,
			source:            "git+https://github.com/acme/tools.git#v1",
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantRemoved:       "selected_scoped_git_checkout",
			wantRetained:      "unrelated_git_checkouts",
		},
		{
			name:              "single segment global git",
			scope:             target.ScopeGlobal,
			source:            "git+https://git.example/repo.git#v1",
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantRemoved:       "selected_scoped_git_checkout",
			wantRetained:      "unrelated_git_checkouts",
		},
		{
			name:              "literal percent global git",
			scope:             target.ScopeGlobal,
			source:            "git+https://example.com/acme/100%25-tool.git#v1",
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantRemoved:       "selected_scoped_git_checkout",
			wantRetained:      "unrelated_git_checkouts",
		},
		{
			name:              "durable credential-bearing git retains git lifecycle",
			scope:             target.ScopeGlobal,
			source:            "git:user:actual-secret@github.com/acme/tools",
			wantPostcondition: effectpostcondition.CarrierArtifactsAbsent,
			wantRemoved:       "selected_scoped_git_checkout",
			wantRetained:      "unrelated_git_checkouts",
		},
		{
			name:              "project local",
			scope:             target.ScopeProject,
			source:            "./packages/tools",
			wantTrust:         true,
			wantPostcondition: effectpostcondition.LocalSourceUnchanged,
			wantRemoved:       "selected_pi_package_relation",
			wantRetained:      "local_source_directory",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(desiredextension.SourceKindHostSource, test.source)
			if err != nil {
				t.Fatal(err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierPiPackage,
				target.TargetPi,
				test.scope,
				source,
			)
			if err != nil {
				t.Fatal(err)
			}
			dossier, admitted, err := routeProfile.RemovalDossier(key)
			if err != nil || !admitted {
				t.Fatalf("RemovalDossier = %#v, %t, %v", dossier, admitted, err)
			}
			requirements := dossier.EffectPostconditions().Requirements()
			if dossier.RequiresExistingTrust() != test.wantTrust ||
				len(requirements) != 1 ||
				requirements[0] != test.wantPostcondition ||
				!slices.Contains(dossier.RemovedEffects(), test.wantRemoved) ||
				!slices.Contains(dossier.RetainedEffects(), test.wantRetained) ||
				(test.wantNonClaim != "" && !slices.Contains(dossier.NonClaims(), test.wantNonClaim)) {
				t.Fatalf("removal dossier = %#v", dossier)
			}
			removed := dossier.RemovedEffects()
			removed[0] = "forged"
			if slices.Contains(dossier.RemovedEffects(), "forged") {
				t.Fatal("removal dossier exposed mutable effects")
			}
		})
	}
}

func TestOpenCodeRemovalDossierOwnsOnlyExactConfigRelations(t *testing.T) {
	routeProfile, ok := Profile(target.TargetOpenCode).DelegatedRoute(
		desiredextension.CarrierOpenCodePlugin,
	)
	if !ok {
		t.Fatal("OpenCode delegated route profile is missing")
	}
	for _, scope := range []target.Scope{target.ScopeProject, target.ScopeGlobal} {
		t.Run(string(scope), func(t *testing.T) {
			source, err := desiredextension.NewSourceRef(
				desiredextension.SourceKindHostSource,
				"@acme/opencode-plugin",
			)
			if err != nil {
				t.Fatal(err)
			}
			key, err := desiredextension.NewCarrierKey(
				desiredextension.CarrierOpenCodePlugin,
				target.TargetOpenCode,
				scope,
				source,
			)
			if err != nil {
				t.Fatal(err)
			}
			dossier, admitted, err := routeProfile.RemovalDossier(key)
			if err != nil || !admitted {
				t.Fatalf("RemovalDossier = %#v, %t, %v", dossier, admitted, err)
			}
			if dossier.Actuation() != RemovalActuationDirectProjection ||
				dossier.RequiresExistingTrust() ||
				dossier.PreservesSharedCarrier() ||
				len(dossier.EffectPostconditions().Requirements()) != 0 ||
				!slices.Contains(
					dossier.RemovedEffects(),
					"selected_opencode_plugin_config_relation",
				) ||
				!slices.Contains(
					dossier.RetainedEffects(),
					"package_manager_installations",
				) ||
				!slices.Contains(
					dossier.NonClaims(),
					"package_or_dependency_uninstall",
				) ||
				!slices.Contains(
					dossier.NonClaims(),
					"unsupported_config_layer_cleanup",
				) {
				t.Fatalf("OpenCode removal dossier = %#v", dossier)
			}
		})
	}
}

func TestRemovalDossierValidationRejectsEmptyDuplicateOrDetachedRows(t *testing.T) {
	valid, ok := Profile(target.TargetPi).DelegatedRoute(desiredextension.CarrierPiPackage)
	if !ok {
		t.Fatal("Pi delegated route profile is missing")
	}

	empty := cloneDelegatedRouteProfile(valid)
	empty.removalDossiers = nil
	if err := empty.Validate(); err == nil || !strings.Contains(err.Error(), "admitted together") {
		t.Fatalf("empty dossier error = %v", err)
	}

	incomplete := cloneDelegatedRouteProfile(valid)
	incomplete.removalDossiers = incomplete.removalDossiers[1:]
	if err := incomplete.Validate(); err == nil || !strings.Contains(err.Error(), "omit") {
		t.Fatalf("incomplete exhaustive dossier error = %v", err)
	}

	duplicate := cloneDelegatedRouteProfile(valid)
	duplicate.removalDossiers = append(
		duplicate.removalDossiers,
		duplicate.removalDossiers[0],
	)
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("duplicate dossier error = %v", err)
	}

	detached := cloneDelegatedRouteProfile(valid)
	routes := make([]OperationRoute, 0, len(detached.operationRoutes)-1)
	for _, route := range detached.operationRoutes {
		if route.Operation() != OperationRemove {
			routes = append(routes, route)
		}
	}
	detached.operationRoutes = routes
	if err := detached.Validate(); err == nil || !strings.Contains(err.Error(), "admitted together") {
		t.Fatalf("detached dossier error = %v", err)
	}

	conflicting := cloneDelegatedRouteProfile(valid)
	dossier := &conflicting.removalDossiers[0].dossier
	dossier.retainedEffects = canonicalStringSet(append(
		dossier.retainedEffects,
		dossier.removedEffects[0],
	))
	if err := conflicting.Validate(); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting effect disclosure error = %v", err)
	}
}

func TestRemovalDossierDisclosureValidation(t *testing.T) {
	valid := DelegatedRemovalDossier{
		actuation:       RemovalActuationHostRoute,
		removedEffects:  []string{"a", "d"},
		retainedEffects: []string{"b", "e"},
		nonClaims:       []string{"c", "f"},
	}
	before := cloneDelegatedRemovalDossier(valid)
	if err := validateDelegatedRemovalDossier(valid); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(valid.removedEffects, before.removedEffects) ||
		!slices.Equal(valid.retainedEffects, before.retainedEffects) ||
		!slices.Equal(valid.nonClaims, before.nonClaims) {
		t.Fatal("validation changed the disclosure sets")
	}

	fields := []struct {
		name   string
		values func(*DelegatedRemovalDossier) *[]string
	}{
		{"removed", func(dossier *DelegatedRemovalDossier) *[]string { return &dossier.removedEffects }},
		{"retained", func(dossier *DelegatedRemovalDossier) *[]string { return &dossier.retainedEffects }},
		{"non-claim", func(dossier *DelegatedRemovalDossier) *[]string { return &dossier.nonClaims }},
	}
	for _, field := range fields {
		for _, invalid := range []struct {
			name   string
			values []string
		}{
			{"empty", nil},
			{"duplicate", []string{"x", "x"}},
			{"unsorted", []string{"z", "x"}},
			{"padded", []string{" x"}},
			{"unstable-token", []string{"x/y"}},
			{"non-ascii-token", []string{"é"}},
		} {
			t.Run(field.name+"/"+invalid.name, func(t *testing.T) {
				dossier := cloneDelegatedRemovalDossier(valid)
				*field.values(&dossier) = slices.Clone(invalid.values)
				if err := validateDelegatedRemovalDossier(dossier); err == nil {
					t.Fatal("invalid disclosure set was accepted")
				}
			})
		}
	}

	for first := 0; first < len(fields); first++ {
		for second := first + 1; second < len(fields); second++ {
			t.Run(fields[first].name+"/conflicts-with/"+fields[second].name, func(t *testing.T) {
				dossier := cloneDelegatedRemovalDossier(valid)
				*fields[first].values(&dossier) = []string{"shared"}
				*fields[second].values(&dossier) = []string{"shared"}
				if err := validateDelegatedRemovalDossier(dossier); err == nil {
					t.Fatal("overlapping disclosure sets were accepted")
				}
			})
		}
	}

	for _, test := range []struct {
		name     string
		removed  []string
		retained []string
		nonClaim []string
		message  string
	}{
		{
			name: "first-conflicting-value", removed: []string{"b"},
			retained: []string{"a"}, nonClaim: []string{"a", "b"},
			message: `removal non-claim "a" conflicts with retained effect`,
		},
		{
			name: "earlier-conflict-before-later-format", removed: []string{"a"},
			retained: []string{"a"}, nonClaim: nil,
			message: `retained effect "a" conflicts with removed effect`,
		},
		{
			name: "current-format-before-conflict", removed: []string{"a"},
			retained: []string{"a", "a"}, nonClaim: []string{"x"},
			message: "retained effect set must be sorted and unique",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			dossier := cloneDelegatedRemovalDossier(valid)
			dossier.removedEffects = test.removed
			dossier.retainedEffects = test.retained
			dossier.nonClaims = test.nonClaim
			if err := validateDelegatedRemovalDossier(dossier); err == nil || err.Error() != test.message {
				t.Fatalf("validation error = %v, want %s", err, test.message)
			}
		})
	}
}

func BenchmarkDelegatedRemovalValidation(b *testing.B) {
	routeProfile, ok := Profile(target.TargetPi).DelegatedRoute(desiredextension.CarrierPiPackage)
	if !ok {
		b.Fatal("Pi delegated route profile is missing")
	}
	dossier := routeProfile.removalDossiers[0].dossier
	for _, benchmark := range []struct {
		name     string
		validate func() error
	}{
		{"dossier", func() error { return validateDelegatedRemovalDossier(dossier) }},
		{"profile", routeProfile.Validate},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := benchmark.validate(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
