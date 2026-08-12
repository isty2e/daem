package adopt

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	targetpkg "github.com/isty2e/daem/internal/target"
)

// CanonicalSourceRoutes validates complete target coverage and returns one
// target-labeled source route per target in stable order. Exact duplicate raw
// observations collapse to one route; distinct observations for one target
// conflict.
func (skill Skill) CanonicalSourceRoutes() ([]SkillSourceRoute, error) {
	targets, err := targetpkg.NewSet(skill.Targets)
	if err != nil {
		return nil, err
	}

	routesByTarget := make(map[targetpkg.Target]SkillSourceRoute, targets.Len())
	for index, route := range skill.SourceRoutes {
		if !targets.Contains(route.Target) {
			return nil, fmt.Errorf("source route target %q is not present in targets", route.Target)
		}
		if strings.TrimSpace(route.LivePath) == "" {
			return nil, fmt.Errorf("source route %d live path is required", index)
		}
		if !filepath.IsAbs(route.ReadPath) || filepath.Clean(route.ReadPath) != route.ReadPath {
			return nil, fmt.Errorf(
				"source route %d read path %q must be canonical and absolute",
				index,
				route.ReadPath,
			)
		}

		if existing, present := routesByTarget[route.Target]; present {
			if existing == route {
				continue
			}
			return nil, fmt.Errorf(
				"target %q has conflicting source routes %q -> %q and %q -> %q",
				route.Target,
				existing.LivePath,
				existing.ReadPath,
				route.LivePath,
				route.ReadPath,
			)
		}
		routesByTarget[route.Target] = route
	}

	canonical := make([]SkillSourceRoute, 0, targets.Len())
	for _, target := range targets.Values() {
		route, present := routesByTarget[target]
		if !present {
			return nil, fmt.Errorf("target %q requires exactly one source route", target)
		}
		canonical = append(canonical, route)
	}
	slices.SortFunc(canonical, compareSkillSourceRoute)
	return canonical, nil
}

func compareSkillSourceRoute(left SkillSourceRoute, right SkillSourceRoute) int {
	return cmp.Or(
		cmp.Compare(left.Target, right.Target),
		cmp.Compare(left.LivePath, right.LivePath),
		cmp.Compare(left.ReadPath, right.ReadPath),
	)
}
