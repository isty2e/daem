package mutation

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
)

func pathAncestors(path string) []string {
	ancestors := make([]string, 0)
	for parent := filepath.Dir(path); parent != path; parent = filepath.Dir(parent) {
		ancestors = append(ancestors, parent)
		path = parent
	}
	return ancestors
}

// normalizePairwiseReference preserves the pre-RMN-01 algorithm as a test oracle.
func (store Store) normalizePairwiseReference(domains []Domain) ([]normalizedDomain, error) {
	paths := make(map[string]*pathDomainFact)
	hosts := make(map[string][2]string)
	routes := make([]Domain, 0)
	for _, domain := range domains {
		if err := domain.access.validate(); err != nil {
			return nil, err
		}
		switch domain.kind {
		case domainLogicalPath, domainPhysicalPath:
			if domain.canonicalPath == "" || domain.pathWitness == "" {
				return nil, fmt.Errorf("mutation path domain is not initialized")
			}
			if pathContains(domain.canonicalPath, store.rootKey) {
				return nil, fmt.Errorf("mutation path %q contains the lease store", domain.canonicalPath)
			}
			fact := paths[domain.canonicalPath]
			if fact == nil {
				fact = &pathDomainFact{
					path: domain.canonicalPath, witness: domain.pathWitness, access: domain.access,
				}
				paths[domain.canonicalPath] = fact
			} else if fact.witness != domain.pathWitness {
				return nil, fmt.Errorf("mutation path %q has contradictory filesystem semantics", domain.canonicalPath)
			} else if domain.access == AccessExclusive {
				fact.access = AccessExclusive
			}
			if domain.kind == domainPhysicalPath {
				hostKey := encodedKey("host", domain.target, domain.scope)
				hosts[hostKey] = [2]string{domain.target, domain.scope}
			}
		case domainHostRoute:
			if err := domain.containment.validate(); err != nil {
				return nil, err
			}
			routes = append(routes, domain)
		default:
			return nil, fmt.Errorf("mutation domain is not initialized")
		}
	}

	for path := range paths {
		for otherPath, other := range paths {
			if path != otherPath && other.access == AccessExclusive && pathContains(otherPath, path) {
				delete(paths, path)
				break
			}
		}
	}

	entries := make(map[string]normalizedDomain)
	add := func(key string, label string, access AccessMode) {
		_, present := entries[key]
		if !present || access == AccessExclusive {
			entries[key] = normalizedDomain{key: key, label: label, access: access}
		}
	}
	for _, fact := range paths {
		add(pathKey(fact.path), strconv.Quote(fact.path), fact.access)
		for _, ancestor := range pathAncestors(fact.path) {
			add(pathKey(ancestor), strconv.Quote(ancestor), AccessShared)
		}
	}
	for _, host := range hosts {
		addHostIntents(add, host[0], host[1])
	}
	for _, route := range routes {
		addHostIntents(add, route.target, route.scope)
		switch route.containment {
		case RouteContainmentCompletePaths:
			key := encodedKey("host-route", route.target, route.scope, route.family)
			add(key, fmt.Sprintf("host route %q/%q/%q", route.target, route.scope, route.family), AccessExclusive)
		case RouteContainmentScope:
			key := encodedKey("host-scope", route.target, route.scope)
			add(key, fmt.Sprintf("host scope %q/%q", route.target, route.scope), AccessExclusive)
		case RouteContainmentUnknown:
			key := encodedKey("host-target", route.target)
			add(key, fmt.Sprintf("host target %q", route.target), AccessExclusive)
		}
	}

	normalized := make([]normalizedDomain, 0, len(entries))
	for _, entry := range entries {
		normalized = append(normalized, entry)
	}
	sort.Slice(normalized, func(left int, right int) bool {
		return normalized[left].key < normalized[right].key
	})
	return normalized, nil
}
