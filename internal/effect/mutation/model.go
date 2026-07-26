// Package mutation provides cross-process mutation identity, revision, and lease primitives.
package mutation

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode"
)

// AccessMode identifies the exclusion level requested for one mutation domain.
type AccessMode uint8

const (
	// AccessShared permits concurrent readers of the same domain.
	AccessShared AccessMode = iota + 1
	// AccessExclusive excludes readers and writers of the same domain.
	AccessExclusive
)

func (mode AccessMode) validate() error {
	switch mode {
	case AccessShared, AccessExclusive:
		return nil
	default:
		return fmt.Errorf("invalid mutation access mode %d", mode)
	}
}

// PathEffect identifies whether a filesystem operation owns a directory entry or its referent.
type PathEffect uint8

const (
	// PathEffectDirectoryEntry preserves the final path component during canonicalization.
	PathEffectDirectoryEntry PathEffect = iota + 1
	// PathEffectReferent resolves the complete existing path through symbolic links.
	PathEffectReferent
)

func (effect PathEffect) validate() error {
	switch effect {
	case PathEffectDirectoryEntry, PathEffectReferent:
		return nil
	default:
		return fmt.Errorf("invalid mutation path effect %d", effect)
	}
}

// RouteContainment records the strongest proven host-route footprint boundary.
type RouteContainment uint8

const (
	// RouteContainmentCompletePaths means the caller supplies every physical path separately.
	RouteContainmentCompletePaths RouteContainment = iota + 1
	// RouteContainmentScope means effects are proven to stay within the target and scope.
	RouteContainmentScope
	// RouteContainmentUnknown means effects may cross scopes for the target.
	RouteContainmentUnknown
)

func (containment RouteContainment) validate() error {
	switch containment {
	case RouteContainmentCompletePaths, RouteContainmentScope, RouteContainmentUnknown:
		return nil
	default:
		return fmt.Errorf("invalid host route containment %d", containment)
	}
}

// LogicalPathRequest describes access to a desired-state or operation-metadata path.
type LogicalPathRequest struct {
	Path   string
	Access AccessMode
	Effect PathEffect
}

// PhysicalPathRequest describes access to a target-visible path.
type PhysicalPathRequest struct {
	Path   string
	Access AccessMode
	Effect PathEffect
	Target string
	Scope  string
}

// PhysicalAuthorityRequest identifies one target-visible path whose mutation
// authority must be covered by an acquired exclusive lease.
type PhysicalAuthorityRequest struct {
	Path   string
	Target string
	Scope  string
}

// PhysicalAuthoritySet is immutable effect-bound physical authority. It is
// deliberately opaque so only LeaseSet can interpret its canonical domains.
type PhysicalAuthoritySet struct {
	domains []Domain
}

// HostRouteRequest describes an opaque host-owned operation realization.
type HostRouteRequest struct {
	Target      string
	Scope       string
	Family      string
	Containment RouteContainment
}

type domainKind uint8

const (
	domainLogicalPath domainKind = iota + 1
	domainPhysicalPath
	domainHostRoute
)

// Domain is a validated mutation lease request. Its identity fields are intentionally private.
type Domain struct {
	kind          domainKind
	access        AccessMode
	canonicalPath string
	requestedPath string
	effect        PathEffect
	target        string
	scope         string
	family        string
	containment   RouteContainment
}

// NewLogicalPathDomain validates and canonicalizes a logical path request.
func NewLogicalPathDomain(request LogicalPathRequest) (Domain, error) {
	if err := request.Access.validate(); err != nil {
		return Domain{}, err
	}
	identity, err := canonicalPathIdentity(request.Path, request.Effect)
	if err != nil {
		return Domain{}, err
	}
	return Domain{
		kind:          domainLogicalPath,
		access:        request.Access,
		canonicalPath: identity.keyPath,
		requestedPath: request.Path,
		effect:        request.Effect,
	}, nil
}

// NewPhysicalPathDomain validates and canonicalizes a target-visible path request.
func NewPhysicalPathDomain(request PhysicalPathRequest) (Domain, error) {
	if err := request.Access.validate(); err != nil {
		return Domain{}, err
	}
	if err := validateRouteFact("target", request.Target); err != nil {
		return Domain{}, err
	}
	if err := validateRouteFact("scope", request.Scope); err != nil {
		return Domain{}, err
	}
	identity, err := canonicalPathIdentity(request.Path, request.Effect)
	if err != nil {
		return Domain{}, err
	}
	return Domain{
		kind:          domainPhysicalPath,
		access:        request.Access,
		canonicalPath: identity.keyPath,
		requestedPath: request.Path,
		effect:        request.Effect,
		target:        request.Target,
		scope:         request.Scope,
	}, nil
}

// NewPhysicalAuthoritySet constructs exact directory-entry and referent
// authority for every effect-bound physical destination.
func NewPhysicalAuthoritySet(requests ...PhysicalAuthorityRequest) (PhysicalAuthoritySet, error) {
	domains := make([]Domain, 0, len(requests)*2)
	for index, request := range requests {
		for _, effect := range []PathEffect{PathEffectDirectoryEntry, PathEffectReferent} {
			domain, err := NewPhysicalPathDomain(PhysicalPathRequest{
				Path: request.Path, Access: AccessExclusive, Effect: effect,
				Target: request.Target, Scope: request.Scope,
			})
			if err != nil {
				return PhysicalAuthoritySet{}, fmt.Errorf("physical authority request[%d]: %w", index, err)
			}
			domains = append(domains, domain)
		}
	}
	return PhysicalAuthoritySet{domains: domains}, nil
}

// CoversPhysicalAuthority reports whether every effect-bound physical domain
// is exactly represented by an acquired exclusive physical lease request.
func (set *LeaseSet) CoversPhysicalAuthority(authority PhysicalAuthoritySet) (bool, error) {
	if set == nil {
		return false, fmt.Errorf("mutation lease set is not initialized")
	}
	set.mu.RLock()
	defer set.mu.RUnlock()
	if set.released || len(set.held) == 0 || len(set.domains) == 0 {
		return false, fmt.Errorf("mutation lease set is not active")
	}

	type coverageKey struct {
		path   string
		effect PathEffect
		target string
		scope  string
	}
	acquired := make(map[coverageKey]struct{}, len(set.domains))
	for _, domain := range set.domains {
		if domain.kind != domainPhysicalPath || domain.access != AccessExclusive {
			continue
		}
		acquired[coverageKey{
			path: domain.canonicalPath, effect: domain.effect,
			target: domain.target, scope: domain.scope,
		}] = struct{}{}
	}
	for _, domain := range authority.domains {
		if domain.kind != domainPhysicalPath || domain.access != AccessExclusive {
			return false, fmt.Errorf("physical authority set is invalid")
		}
		key := coverageKey{
			path: domain.canonicalPath, effect: domain.effect,
			target: domain.target, scope: domain.scope,
		}
		if _, covered := acquired[key]; !covered {
			return false, nil
		}
	}
	return true, nil
}

func (domain Domain) matchesCurrentPath() (bool, error) {
	switch domain.kind {
	case domainLogicalPath, domainPhysicalPath:
		identity, err := canonicalPathIdentity(domain.requestedPath, domain.effect)
		if err != nil {
			return false, err
		}
		return identity.keyPath == domain.canonicalPath, nil
	case domainHostRoute:
		return true, nil
	default:
		return false, fmt.Errorf("mutation domain is not initialized")
	}
}

// NewHostRouteDomain validates an opaque host route request.
func NewHostRouteDomain(request HostRouteRequest) (Domain, error) {
	if err := validateRouteFact("target", request.Target); err != nil {
		return Domain{}, err
	}
	if err := validateRouteFact("scope", request.Scope); err != nil {
		return Domain{}, err
	}
	if err := validateRouteFact("route family", request.Family); err != nil {
		return Domain{}, err
	}
	if err := request.Containment.validate(); err != nil {
		return Domain{}, err
	}
	return Domain{
		kind:        domainHostRoute,
		access:      AccessExclusive,
		target:      request.Target,
		scope:       request.Scope,
		family:      request.Family,
		containment: request.Containment,
	}, nil
}

func validateRouteFact(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("mutation %s is required", name)
	}
	if value != strings.TrimSpace(value) {
		return fmt.Errorf("mutation %s contains surrounding whitespace", name)
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return fmt.Errorf("mutation %s contains a control character", name)
	}
	return nil
}

// OperationFingerprint is an immutable digest of workflow-owned canonical plan bytes.
type OperationFingerprint struct {
	digest [sha256.Size]byte
	valid  bool
}

// NewOperationFingerprint computes a fingerprint without retaining the input bytes.
func NewOperationFingerprint(canonical []byte) OperationFingerprint {
	return OperationFingerprint{digest: sha256.Sum256(canonical), valid: true}
}

// Equal reports whether two operation fingerprints identify the same canonical bytes.
func (fingerprint OperationFingerprint) Equal(other OperationFingerprint) bool {
	return fingerprint.valid && other.valid && fingerprint.digest == other.digest
}
