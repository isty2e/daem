package adopt

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

func skillSourceAuthorities(skills []Skill) []SkillSourceAuthority {
	authorities := make([]SkillSourceAuthority, 0, len(skills))
	for _, skill := range skills {
		authorities = append(authorities, skill.sourceAuthority())
	}
	return authorities
}

func canonicalSkillSourceAuthorities(values []SkillSourceAuthority) []SkillSourceAuthority {
	canonical := cloneSkillSourceAuthorities(values)
	slices.SortFunc(canonical, compareSkillSourceAuthority)
	unique := canonical[:0]
	for _, authority := range canonical {
		if len(unique) == 0 || compareSkillSourceAuthority(unique[len(unique)-1], authority) != 0 {
			unique = append(unique, authority)
		}
	}
	return unique
}

func validateSkillSourceAuthorities(authorities []SkillSourceAuthority, skills []Skill) error {
	type subject struct {
		resourceName string
		scope        string
	}
	available := make(map[subject]SkillSourceAuthority, len(authorities))
	for index, authority := range authorities {
		if err := authority.validate(); err != nil {
			return fmt.Errorf("skill source authority %d: %w", index, err)
		}
		key := subject{resourceName: authority.ResourceName, scope: string(authority.Scope)}
		if existing, exists := available[key]; exists && compareSkillSourceAuthority(existing, authority) != 0 {
			return fmt.Errorf(
				"skill source authority %q in scope %q carries conflicting exact evidence",
				authority.ResourceName,
				authority.Scope,
			)
		}
		available[key] = authority
	}
	for index, skill := range skills {
		expected := skill.sourceAuthority()
		key := subject{resourceName: expected.ResourceName, scope: string(expected.Scope)}
		actual, exists := available[key]
		if !exists || compareSkillSourceAuthority(actual, expected) != 0 {
			return fmt.Errorf("skill candidate %d has no source authority", index)
		}
	}
	return nil
}

func (authority SkillSourceAuthority) validate() error {
	if strings.TrimSpace(authority.ResourceName) == "" {
		return fmt.Errorf("resource name is required")
	}
	if err := authority.ContentHash.Validate(); err != nil {
		return fmt.Errorf("content hash: %w", err)
	}
	if len(authority.Routes) == 0 {
		return fmt.Errorf("at least one source route is required")
	}
	for index, route := range authority.Routes {
		if err := validateTargetScope(route.Target, authority.Scope); err != nil {
			return fmt.Errorf("source route %d: %w", index, err)
		}
		if strings.TrimSpace(route.LivePath) == "" {
			return fmt.Errorf("source route %d live path is required", index)
		}
		if !filepath.IsAbs(route.ReadPath) || filepath.Clean(route.ReadPath) != route.ReadPath {
			return fmt.Errorf(
				"source route %d read path %q must be canonical and absolute",
				index,
				route.ReadPath,
			)
		}
		if index > 0 && string(authority.Routes[index-1].Target) >= string(route.Target) {
			return fmt.Errorf("source routes must be sorted with unique targets")
		}
	}
	return nil
}

func compareSkillSourceAuthority(left SkillSourceAuthority, right SkillSourceAuthority) int {
	return cmp.Or(
		cmp.Compare(left.ResourceName, right.ResourceName),
		cmp.Compare(left.Scope, right.Scope),
		cmp.Compare(left.ContentHash, right.ContentHash),
		slices.CompareFunc(left.Routes, right.Routes, compareSkillSourceRoute),
	)
}

func cloneSkillSourceAuthorities(values []SkillSourceAuthority) []SkillSourceAuthority {
	if values == nil {
		return nil
	}
	cloned := make([]SkillSourceAuthority, len(values))
	copy(cloned, values)
	for index := range cloned {
		cloned[index].Routes = append([]SkillSourceRoute(nil), cloned[index].Routes...)
	}
	return cloned
}
