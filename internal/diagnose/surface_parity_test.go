package diagnose

import (
	"slices"
	"testing"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization/profile"
	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestCompiledSkillRootSpecsMatchLegacyProfiles(t *testing.T) {
	t.Parallel()

	for _, selectedTarget := range targetpkg.SupportedTargets() {
		for _, scope := range []targetpkg.Scope{targetpkg.ScopeProject, targetpkg.ScopeGlobal} {
			got := skillRootSpecs(selectedTarget, scope)
			want := legacySkillRootSpecs(profile.Profile(selectedTarget), scope)
			if !slices.Equal(got, want) {
				t.Fatalf("%s/%s skill roots = %#v, want %#v", selectedTarget, scope, got, want)
			}
		}
		for _, kind := range []entity.Kind{entity.KindInstructions, entity.KindSkill} {
			got := defaultPlacementScopes(selectedTarget, kind)
			want := legacyDefaultPlacementScopes(profile.Profile(selectedTarget), kind)
			if !slices.Equal(got, want) {
				t.Fatalf("%s/%s default scopes = %#v, want %#v", selectedTarget, kind, got, want)
			}
		}
	}
}

func legacySkillRootSpecs(targetProfile profile.TargetProfile, scope targetpkg.Scope) []doctorSkillRootSpec {
	result := make([]doctorSkillRootSpec, 0)
	defaultPlacement, err := targetProfile.DefaultPlacement(entity.KindSkill, scope)
	if err == nil {
		result = append(result, doctorSkillRootSpec{
			Scope: scope, Role: "preferred", Index: -1, Root: defaultPlacement.Root().String(),
		})
	}
	compatibleIndex := 0
	for _, location := range targetProfile.DiscoveryLocations(entity.KindSkill, scope) {
		if admission, admitted := targetProfile.PlacementAdmissionAt(
			entity.KindSkill,
			scope,
			location.Path(),
		); admitted && admission.Default() {
			continue
		}
		result = append(result, doctorSkillRootSpec{
			Scope: scope, Role: "compatible", Index: compatibleIndex, Root: location.Path(),
		})
		compatibleIndex++
	}
	return result
}

func legacyDefaultPlacementScopes(
	targetProfile profile.TargetProfile,
	resourceKind entity.Kind,
) []targetpkg.Scope {
	scopes := make([]targetpkg.Scope, 0, 2)
	for _, scope := range []targetpkg.Scope{targetpkg.ScopeProject, targetpkg.ScopeGlobal} {
		if _, err := targetProfile.DefaultPlacement(resourceKind, scope); err == nil {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}
