package adopt

import (
	"fmt"
	"sort"
	"strings"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func mcpSourceAuthorities(servers []MCPServer) []MCPSourceAuthority {
	authorities := make([]MCPSourceAuthority, 0, len(servers))
	for _, server := range servers {
		authorities = append(authorities, server.sourceAuthority())
	}
	return authorities
}

func canonicalMCPSourceAuthorities(values []MCPSourceAuthority) []MCPSourceAuthority {
	canonical := cloneMCPSourceAuthorities(values)
	sort.Slice(canonical, func(left int, right int) bool {
		return compareMCPSourceAuthority(canonical[left], canonical[right]) < 0
	})
	unique := canonical[:0]
	for _, authority := range canonical {
		if len(unique) == 0 || compareMCPSourceAuthority(unique[len(unique)-1], authority) != 0 {
			unique = append(unique, authority)
		}
	}
	return unique
}

func validateMCPSourceAuthorities(authorities []MCPSourceAuthority, servers []MCPServer) error {
	available := make(map[mcpSourceAuthoritySubject]MCPSourceAuthority, len(authorities))
	type primarySourceFact struct {
		revision     string
		maximumBytes int64
	}
	primarySources := make(map[string]primarySourceFact)
	requiredAbsent := make(map[string]struct{})
	for index, authority := range authorities {
		if err := authority.validate(); err != nil {
			return fmt.Errorf("mcp source authority %d: %w", index, err)
		}
		subject := mcpSourceAuthoritySubjectOf(authority)
		if existing, exists := available[subject]; exists && compareMCPSourceAuthority(existing, authority) != 0 {
			return fmt.Errorf("mcp source authority %q carries conflicting exact revisions", authority.PrimaryPath)
		}
		available[subject] = authority
		primaryFact := primarySourceFact{
			revision:     authority.PrimaryRevision,
			maximumBytes: authority.MaximumBytes,
		}
		if existing, exists := primarySources[authority.PrimaryPath]; exists && existing != primaryFact {
			return fmt.Errorf("MCP primary source %q carries conflicting exact revisions", authority.PrimaryPath)
		}
		if _, conflicts := requiredAbsent[authority.PrimaryPath]; conflicts {
			return fmt.Errorf("MCP source path %q cannot be both present and required absent", authority.PrimaryPath)
		}
		primarySources[authority.PrimaryPath] = primaryFact
		for _, path := range authority.RequiredAbsentPaths {
			if _, conflicts := primarySources[path]; conflicts {
				return fmt.Errorf("MCP source path %q cannot be both present and required absent", path)
			}
			requiredAbsent[path] = struct{}{}
		}
	}
	for index, server := range servers {
		expected := server.sourceAuthority()
		actual, exists := available[mcpSourceAuthoritySubjectOf(expected)]
		if !exists || compareMCPSourceAuthority(actual, expected) != 0 {
			return fmt.Errorf("mcp server candidate %d has no source authority", index)
		}
	}
	return nil
}

type mcpSourceAuthoritySubject struct {
	target      targetpkg.Target
	scope       targetpkg.Scope
	primaryPath string
}

func mcpSourceAuthoritySubjectOf(authority MCPSourceAuthority) mcpSourceAuthoritySubject {
	return mcpSourceAuthoritySubject{
		target:      authority.Target,
		scope:       authority.Scope,
		primaryPath: authority.PrimaryPath,
	}
}

func compareMCPSourceAuthority(left MCPSourceAuthority, right MCPSourceAuthority) int {
	for _, pair := range [][2]string{
		{string(left.Target), string(right.Target)},
		{string(left.Scope), string(right.Scope)},
		{left.PrimaryPath, right.PrimaryPath},
		{left.PrimaryRevision, right.PrimaryRevision},
	} {
		if comparison := strings.Compare(pair[0], pair[1]); comparison != 0 {
			return comparison
		}
	}
	if left.MaximumBytes < right.MaximumBytes {
		return -1
	}
	if left.MaximumBytes > right.MaximumBytes {
		return 1
	}
	for index := 0; index < len(left.RequiredAbsentPaths) && index < len(right.RequiredAbsentPaths); index++ {
		if comparison := strings.Compare(
			left.RequiredAbsentPaths[index],
			right.RequiredAbsentPaths[index],
		); comparison != 0 {
			return comparison
		}
	}
	return len(left.RequiredAbsentPaths) - len(right.RequiredAbsentPaths)
}

func cloneMCPSourceAuthorities(values []MCPSourceAuthority) []MCPSourceAuthority {
	if values == nil {
		return nil
	}
	cloned := make([]MCPSourceAuthority, len(values))
	copy(cloned, values)
	for index := range cloned {
		cloned[index].RequiredAbsentPaths = cloneStrings(cloned[index].RequiredAbsentPaths)
	}
	return cloned
}
