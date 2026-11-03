package operationplan

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/isty2e/daem/internal/effect/mutation"
)

// RevisionPolicy selects which path facts emit revision requests.
type RevisionPolicy uint8

const (
	// RevisionsOff records facts and domains without revision requests.
	RevisionsOff RevisionPolicy = iota
	// RevisionsFirstEffect records bounded-content revisions except declaration paths.
	RevisionsFirstEffect
	// RevisionsRefreshFull records file revisions for declaration paths and
	// bounded-content revisions for every other path fact that produces a domain.
	RevisionsRefreshFull
)

// Builder accumulates authority facts, pure domain steps, and revision requests.
// Domain order is admission order. Fingerprint marshal sorts a copy.
type Builder struct {
	policy             RevisionPolicy
	declarationFileMax int64
	declarationPaths   map[string]struct{}
	facts              []Fact
	domainSteps        []DomainStep
	revisions          []mutation.RevisionRequest
	revisionKeys       map[string]int
	routeKeys          map[string]struct{}
}

// NewBuilder returns an empty authority builder. declarationFileMax is used
// only for RevisionsRefreshFull declaration-path file revisions.
func NewBuilder(policy RevisionPolicy, declarationPaths []string, declarationFileMax int64) *Builder {
	index := make(map[string]struct{}, len(declarationPaths))
	for _, path := range declarationPaths {
		index[path] = struct{}{}
	}
	return &Builder{
		policy:             policy,
		declarationFileMax: declarationFileMax,
		declarationPaths:   index,
		revisionKeys:       make(map[string]int),
		routeKeys:          make(map[string]struct{}),
	}
}

// AddLogical records a logical path fact and exclusive-or-shared domain step.
func (builder *Builder) AddLogical(path string, access mutation.AccessMode, effect mutation.PathEffect) error {
	return builder.addPathFact(FactLogical, path, access, effect, "", "", "", 0, "", true)
}

// AddLogicalPair records directory-entry and referent logical facts.
func (builder *Builder) AddLogicalPair(path string, entryAccess, referentAccess mutation.AccessMode) error {
	if err := builder.AddLogical(path, entryAccess, mutation.PathEffectDirectoryEntry); err != nil {
		return err
	}
	return builder.AddLogical(path, referentAccess, mutation.PathEffectReferent)
}

// AddOwnershipRegistry retains store lease domains while binding scoped
// semantic evidence instead of the shared file's approval-time bytes.
func (builder *Builder) AddOwnershipRegistry(path string, access mutation.AccessMode, identity string) error {
	if identity == "" {
		return fmt.Errorf("ownership Registry requires its scoped semantic identity")
	}
	for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
		if err := builder.addPathFact(FactOwnershipRegistry, path, access, effect, "", "", "", 0, identity, true); err != nil {
			return err
		}
	}
	return nil
}

// AddDocumentWritePair distinguishes mutation/baseline intent from an
// exclusive physical observation that must not become a protection scope.
func (builder *Builder) AddDocumentWritePair(path, targetName, scope string) error {
	for _, effect := range []mutation.PathEffect{mutation.PathEffectDirectoryEntry, mutation.PathEffectReferent} {
		if err := builder.addPathFact(FactDocumentWrite, path, mutation.AccessExclusive, effect, targetName, scope, "", 0, "", true); err != nil {
			return err
		}
	}
	return nil
}

// AddPhysical records a physical path fact and domain step, including target/scope.
func (builder *Builder) AddPhysical(
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
	targetName string,
	scope string,
) error {
	return builder.addPathFact(FactPhysical, path, access, effect, targetName, scope, "", 0, "", true)
}

// AddPhysicalPair records directory-entry and referent physical facts.
func (builder *Builder) AddPhysicalPair(
	path string,
	access mutation.AccessMode,
	targetName string,
	scope string,
) error {
	if err := builder.AddPhysical(path, access, mutation.PathEffectDirectoryEntry, targetName, scope); err != nil {
		return err
	}
	return builder.AddPhysical(path, access, mutation.PathEffectReferent, targetName, scope)
}

// AddRoute records a host-route fact. Duplicate route keys are ignored.
func (builder *Builder) AddRoute(targetName string, scope string, family string, containment mutation.RouteContainment) error {
	fact := Fact{
		kind:        FactRoute,
		target:      targetName,
		scope:       scope,
		family:      family,
		containment: containment,
	}
	if err := fact.kind.validate(); err != nil {
		return err
	}
	key := fact.coverKey()
	if _, exists := builder.routeKeys[key]; exists {
		return nil
	}
	domain, err := mutation.NewHostRouteDomain(mutation.HostRouteRequest{
		Target:      targetName,
		Scope:       scope,
		Family:      family,
		Containment: containment,
	})
	if err != nil {
		return err
	}
	builder.routeKeys[key] = struct{}{}
	builder.facts = append(builder.facts, fact)
	builder.domainSteps = append(builder.domainSteps, newCompiledDomainStep(domain))
	return nil
}

// AddFingerprintOnly records a fact that participates in fingerprint marshal
// without a mutation domain or revision request.
func (builder *Builder) AddFingerprintOnly(
	kind FactKind,
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
	identity string,
) error {
	if kind.producesDomain() {
		return fmt.Errorf("operationplan: fingerprint-only fact %d produces a domain", kind)
	}
	return builder.addPathFact(kind, path, access, effect, "", "", "", 0, identity, false)
}

// AddRemovalContinuation records a logical exclusive domain and continuation fact.
func (builder *Builder) AddRemovalContinuation(path string, effect mutation.PathEffect) error {
	return builder.addPathFact(
		FactRemovalContinuation,
		path,
		mutation.AccessExclusive,
		effect,
		"",
		"",
		"",
		0,
		"",
		true,
	)
}

// AddDomains appends already constructed domains, preserving admission order.
func (builder *Builder) AddDomains(domains []mutation.Domain) {
	for _, domain := range domains {
		builder.domainSteps = append(builder.domainSteps, newCompiledDomainStep(domain))
	}
}

// AddRevision records a revision request, replacing an earlier request with the
// same (effect, path) key.
func (builder *Builder) AddRevision(request mutation.RevisionRequest) {
	key := revisionMapKey(request)
	if index, exists := builder.revisionKeys[key]; exists {
		builder.revisions[index] = request
		return
	}
	builder.revisionKeys[key] = len(builder.revisions)
	builder.revisions = append(builder.revisions, request)
}

func (builder *Builder) addPathFact(
	kind FactKind,
	path string,
	access mutation.AccessMode,
	effect mutation.PathEffect,
	targetName string,
	scope string,
	family string,
	containment mutation.RouteContainment,
	identity string,
	withDomain bool,
) error {
	if err := kind.validate(); err != nil {
		return err
	}
	fact := Fact{
		kind:        kind,
		path:        path,
		access:      access,
		effect:      effect,
		target:      targetName,
		scope:       scope,
		family:      family,
		containment: containment,
		identity:    identity,
	}
	builder.facts = append(builder.facts, fact)
	if !withDomain {
		return nil
	}
	switch kind {
	case FactLogical, FactRemovalContinuation, FactOwnershipRegistry:
		builder.domainSteps = append(builder.domainSteps, newPathDomainStep(
			newLogicalPathDomainRequest(path, access, effect),
		))
	case FactPhysical, FactDocumentWrite:
		builder.domainSteps = append(builder.domainSteps, newPathDomainStep(
			newPhysicalPathDomainRequest(path, access, effect, targetName, scope),
		))
	default:
		return fmt.Errorf("operationplan: fact %d has no path domain", kind)
	}
	if kind == FactOwnershipRegistry {
		return nil
	}
	return builder.maybeAddRevision(path, effect)
}

func (builder *Builder) maybeAddRevision(path string, effect mutation.PathEffect) error {
	switch builder.policy {
	case RevisionsOff:
		return nil
	case RevisionsFirstEffect:
		if _, declaration := builder.declarationPaths[path]; declaration {
			return nil
		}
		builder.AddRevision(mutation.NewBoundedContentRevisionRequest(path, effect))
		return nil
	case RevisionsRefreshFull:
		if _, declaration := builder.declarationPaths[path]; declaration {
			request, err := mutation.NewBoundedFileRevisionRequest(
				builder.declarationFileMax,
				path,
				effect,
			)
			if err != nil {
				return err
			}
			builder.AddRevision(request)
			return nil
		}
		builder.AddRevision(mutation.NewBoundedContentRevisionRequest(path, effect))
		return nil
	default:
		return fmt.Errorf("operationplan: invalid revision policy %d", builder.policy)
	}
}

func revisionMapKey(request mutation.RevisionRequest) string {
	return strconv.Itoa(int(request.Effect)) + ":" + request.Path
}

func sortedRevisionRequests(requests []mutation.RevisionRequest) []mutation.RevisionRequest {
	out := append([]mutation.RevisionRequest(nil), requests...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Effect != out[j].Effect {
			return out[i].Effect < out[j].Effect
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Plan is the compiled authority algebra: admission-ordered domain steps,
// role-sorted revisions, and the closed fact set used by fingerprint projections.
type Plan struct {
	facts       []Fact
	domainSteps []DomainStep
	revisions   []mutation.RevisionRequest
}

// Facts returns a copy of compiled authority facts in admission order.
func (plan Plan) Facts() []Fact {
	return append([]Fact(nil), plan.facts...)
}

// DocumentWriteRequests excludes read-only/exclusive observations and control
// paths. Both mutation and new ownership-baseline intents select documents.
func (plan Plan) DocumentWriteRequests() []mutation.PhysicalAuthorityRequest {
	seen := make(map[mutation.PhysicalAuthorityRequest]struct{})
	var requests []mutation.PhysicalAuthorityRequest
	for _, fact := range plan.facts {
		request, write := fact.documentWrite()
		if !write {
			continue
		}
		if _, duplicate := seen[request]; !duplicate {
			requests = append(requests, request)
			seen[request] = struct{}{}
		}
	}
	return requests
}

// DomainSteps returns mutation-domain lowering steps in admission order.
func (plan Plan) DomainSteps() []DomainStep {
	return append([]DomainStep(nil), plan.domainSteps...)
}

// Revisions returns revision requests sorted by (effect, path).
func (plan Plan) Revisions() []mutation.RevisionRequest {
	return append([]mutation.RevisionRequest(nil), plan.revisions...)
}

// RevisionsForPaths returns the compiled revisions whose paths are in selected,
// preserving compiled sort order.
func (plan Plan) RevisionsForPaths(selected map[string]struct{}) []mutation.RevisionRequest {
	if len(selected) == 0 {
		return nil
	}
	out := make([]mutation.RevisionRequest, 0)
	for _, request := range plan.revisions {
		if _, ok := selected[request.Path]; ok {
			out = append(out, request)
		}
	}
	return out
}

// Compile freezes admission-ordered domain steps, sorted revisions, and facts.
func (builder *Builder) Compile() Plan {
	return Plan{
		facts:       append([]Fact(nil), builder.facts...),
		domainSteps: append([]DomainStep(nil), builder.domainSteps...),
		revisions:   sortedRevisionRequests(builder.revisions),
	}
}
