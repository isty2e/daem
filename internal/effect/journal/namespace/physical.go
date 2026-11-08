package namespace

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

type physicalDocumentFacts struct {
	global  []output.RootReference
	roles   []configuration.DocumentSpec
	unknown []*ProtectionError
}

var ErrPhysicalRulesChanged = errors.New("native document role or global namespace changed after claim planning")

func (facts physicalDocumentFacts) equal(other physicalDocumentFacts) bool {
	if !slices.Equal(facts.global, other.global) || !slices.Equal(facts.roles, other.roles) || len(facts.unknown) != len(other.unknown) {
		return false
	}
	for index, left := range facts.unknown {
		right := other.unknown[index]
		if left.reference != right.reference || left.host != right.host || left.source != right.source || left.Endpoint() != right.Endpoint() || left.Uncertainty() != right.Uncertainty() {
			return false
		}
	}
	return true
}

func documentRoleRelation(left, right mutation.DirectoryEntryAuthorityObservation) protectionRelation {
	leftExact, leftKnown := left.Exact()
	rightExact, rightKnown := right.Exact()
	if leftKnown && rightKnown {
		if leftExact.Equal(rightExact) {
			return protectionOverlap
		}
		return protectionDisjoint
	}
	if !leftKnown && rightKnown {
		provisional, _ := left.Provisional()
		if !provisional.CouldEqual(rightExact) {
			return protectionDisjoint
		}
	}
	if leftKnown && !rightKnown {
		provisional, _ := right.Provisional()
		if !provisional.CouldEqual(leftExact) {
			return protectionDisjoint
		}
	}
	if protectionPathRelation(left, right) == protectionDisjoint {
		return protectionDisjoint
	}
	return protectionIndeterminate
}

func (namespace DocumentNamespace) GlobalLocations() []output.RootReference {
	return slices.Clone(namespace.physical.global)
}

func (namespace DocumentNamespace) PhysicalRoles() []configuration.DocumentSpec {
	return slices.Clone(namespace.physical.roles)
}

func (namespace DocumentNamespace) RequireCompletePhysicalRules() error {
	if err := namespace.observation.Validate(); err != nil {
		return fmt.Errorf("native physical rules require an observed document: %w", err)
	}
	var failures protectionFailures
	for _, unknown := range namespace.physical.unknown {
		failures.add(unknown)
	}
	return failures.result()
}

// RequirePhysicalRules preserves the planning observation across forward journal
// reloads. It does not compare provisional and promoted referent identities.
func (namespace DocumentNamespace) RequirePhysicalRules(expected DocumentNamespace) error {
	if err := expected.observation.Validate(); err != nil {
		return fmt.Errorf("original native rule observation is required: %w", err)
	}
	if !slices.Equal(namespace.Addresses(), expected.Addresses()) || !namespace.physical.equal(expected.physical) {
		return ErrPhysicalRulesChanged
	}
	return nil
}

type physicalRootObservation struct {
	path        string
	observation mutation.DirectoryEntryAuthorityObservation
	err         error
}

type physicalDirectoryObservation struct {
	names rootedpath.DirectoryNames
	err   error
}

type physicalBudgetRefusal struct{ cause error }

func (failure *physicalBudgetRefusal) Error() string { return failure.cause.Error() }
func (failure *physicalBudgetRefusal) Unwrap() error { return failure.cause }

type physicalObservationBudget struct {
	traversal rootedpath.PhysicalTraversalBudget
	work      rootedpath.DirectoryObservationBudget
}

func (budget *physicalObservationBudget) AdmitPathComponents(count int) error {
	if err := budget.traversal.AdmitPathComponents(count); err != nil {
		return &physicalBudgetRefusal{cause: err}
	}
	return nil
}

func (budget *physicalObservationBudget) AdmitPhysicalWork(components, entries int, bytes int64) error {
	if budget.work == nil {
		return fmt.Errorf("native physical observation requires shared path-and-entry allocation")
	}
	if err := budget.work.AdmitPhysicalWork(components, entries, bytes); err != nil {
		return &physicalBudgetRefusal{cause: err}
	}
	return nil
}

// One observation shares root interpretation across reservations, namespace
// membership and document roles. Nothing here supplies a writer grant.
type physicalObserver struct {
	selectors   ReservationSelectors
	budget      *physicalObservationBudget
	roots       map[output.RootReference]physicalRootObservation
	directories map[string]physicalDirectoryObservation
}

func newPhysicalObserver(selectors ReservationSelectors, budget rootedpath.PhysicalTraversalBudget) *physicalObserver {
	work, _ := budget.(rootedpath.DirectoryObservationBudget)
	return &physicalObserver{
		selectors:   selectors,
		budget:      &physicalObservationBudget{traversal: budget, work: work},
		roots:       make(map[output.RootReference]physicalRootObservation),
		directories: make(map[string]physicalDirectoryObservation),
	}
}

func (observer *physicalObserver) directory(ctx context.Context, path string) (rootedpath.DirectoryNames, error) {
	if err := ctx.Err(); err != nil {
		return rootedpath.DirectoryNames{}, err
	}
	if observer.budget.work == nil {
		return rootedpath.DirectoryNames{}, fmt.Errorf("native physical observation requires shared path-and-entry allocation")
	}
	if previous, present := observer.directories[path]; present {
		return previous.names, previous.err
	}
	names, err := rootedpath.ReadDirectoryNamesBounded(ctx, path, physicalwork.MaximumPhysicalPathDepth, observer.budget)
	observer.directories[path] = physicalDirectoryObservation{names: names, err: err}
	return names, err
}

func (observer *physicalObserver) root(selector ReservationSelector) physicalRootObservation {
	if value, present := observer.roots[selector.reference]; present {
		return value
	}
	value := physicalRootObservation{err: selector.failure}
	if value.err == nil {
		value.path, value.err = rootedpath.ResolveFutureReferentPathBounded(selector.Path(), physicalwork.MaximumPhysicalPathDepth, observer.budget)
	}
	if value.err == nil {
		value.observation, value.err = mutation.ObserveDirectoryEntryAuthorityBounded(value.path, physicalwork.MaximumPhysicalPathDepth, observer.budget)
	}
	observer.roots[selector.reference] = value
	return value
}

func (observer *physicalObserver) documents(ctx context.Context, aliases []Alias, roots QualifiedRoots) (physicalDocumentFacts, error) {
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		return physicalDocumentFacts{}, err
	}
	var result physicalDocumentFacts
	retain := func(facts physicalDocumentFacts, err error) error {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		if err != nil {
			var failure *ProtectionError
			if !errors.As(err, &failure) || failure.Kind() != ProtectionUnverified || !failure.documents {
				return err
			}
			result.unknown = append(result.unknown, failure)
		}
		result.global = append(result.global, facts.global...)
		result.roles = append(result.roles, facts.roles...)
		return nil
	}
	for _, contract := range contracts {
		selector, supplied := observer.selectors.selectors[contract.Reference()]
		if !supplied {
			continue // Unknown host inventory is not admission or checked-empty coverage.
		}
		if selector.absent {
			continue
		}
		facts, err := observer.rootDocuments(ctx, aliases, selector, contract.Host(), contract.Envelope())
		if err := retain(facts, err); err != nil {
			return physicalDocumentFacts{}, err
		}
	}
	if manifest, present := roots.Manifest(); present {
		reference, err := configuration.ProjectRootReference()
		if err != nil {
			return physicalDocumentFacts{}, err
		}
		selector, err := NewReservationSelector(reference, manifest.SelectedPath())
		if err != nil {
			return physicalDocumentFacts{}, err
		}
		facts, err := observer.rootDocuments(ctx, aliases, selector, "", 0)
		if err := retain(facts, err); err != nil {
			return physicalDocumentFacts{}, err
		}
	}
	slices.SortFunc(result.global, output.RootReference.Compare)
	result.global = slices.Compact(result.global)
	slices.SortFunc(result.roles, func(left, right configuration.DocumentSpec) int {
		return cmp.Or(left.RootReference().Compare(right.RootReference()), cmp.Compare(left.Target(), right.Target()), cmp.Compare(left.Scope(), right.Scope()), cmp.Compare(left.RootRelativePath(), right.RootRelativePath()), cmp.Compare(left.PolicyID(), right.PolicyID()))
	})
	result.roles = slices.Compact(result.roles)
	return result, ctx.Err()
}

func (observer *physicalObserver) rootDocuments(ctx context.Context, aliases []Alias, selector ReservationSelector, host target.Target, envelope configuration.NativeEnvelope) (physicalDocumentFacts, error) {
	failure := func(selected target.Target, reason ProtectionUncertainty, endpoint string, cause error) error {
		return &ProtectionError{kind: ProtectionUnverified, uncertainty: reason, address: aliases[0].address, host: selected, reference: selector.reference, source: selector.source, endpoint: endpoint, documents: true, cause: cause}
	}
	if err := ctx.Err(); err != nil {
		return physicalDocumentFacts{}, err
	}
	root := observer.root(selector)
	if root.err != nil {
		return physicalDocumentFacts{}, failure(host, ProtectionLocationUnavailable, "", root.err)
	}
	var result physicalDocumentFacts
	if envelope == configuration.NativeDirectory {
		for _, alias := range aliases {
			route, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(alias.root.SelectedPath(), filepath.FromSlash(alias.relative.Path())), physicalwork.MaximumPhysicalPathDepth, observer.budget)
			if err != nil {
				return result, failure(host, ProtectionDocumentUnavailable, "", err)
			}
			for _, selected := range []mutation.DirectoryEntryAuthorityObservation{route, alias.observation} {
				key, exact := root.observation.Exact()
				candidate, candidateExact := selected.Exact()
				if exact && candidateExact && key.Contains(candidate) {
					result.global = append(result.global, selector.reference)
				} else if exact && !candidateExact {
					provisional, _ := selected.Provisional()
					if provisional.ConfinedWithin(key) {
						result.global = append(result.global, selector.reference)
					} else if provisional.CouldOverlap(key) {
						return result, failure(host, ProtectionRegionIndeterminate, "", fmt.Errorf("native namespace membership is normalization-sensitive"))
					}
				} else if !exact && protectionPathRelation(root.observation, selected) != protectionDisjoint {
					return result, failure(host, ProtectionRegionIndeterminate, "", fmt.Errorf("native namespace membership is normalization-sensitive"))
				}
			}
		}
	}
	var names []string
	if configuration.RootDocumentsRequireNames(selector.reference) {
		if observer.budget.work == nil {
			return result, failure(host, ProtectionAllocationUnavailable, "", fmt.Errorf("native physical observation requires shared path-and-entry allocation"))
		}
		entries, err := observer.directory(ctx, root.path)
		if err != nil {
			if canceled := ctx.Err(); canceled != nil {
				return physicalDocumentFacts{}, canceled
			}
			return result, failure(host, ProtectionLocationUnavailable, "", err)
		}
		names = entries.Names()
		if key, exact := root.observation.Exact(); exact {
			for _, alias := range aliases {
				if selected, exact := alias.observation.Exact(); exact {
					if name, immediate := key.ImmediateChildName(selected); immediate {
						names = append(names, name)
					}
				}
			}
		}
	}
	specs, err := configuration.KnownRootDocuments(selector.reference, names)
	if err != nil {
		return physicalDocumentFacts{}, err
	}
	if configuration.RootDocumentsRequireNames(selector.reference) {
		key, exact := root.observation.Exact()
		if !exact {
			return result, failure(host, ProtectionRegionIndeterminate, "", fmt.Errorf("native document names have no canonical root key"))
		}
		// Stored casing and alias keys share one name space before spec identity
		// is formed. The route keeps outbound links associated with their names.
		canonical := make([]string, 0, len(specs))
		for _, spec := range specs {
			route, err := mutation.ObserveDirectoryEntryAuthorityBounded(filepath.Join(root.path, filepath.FromSlash(spec.RootRelativePath())), physicalwork.MaximumPhysicalPathDepth, observer.budget)
			if err != nil {
				return result, failure(spec.Target(), ProtectionEndpointUnavailable, spec.RootRelativePath(), err)
			}
			entry, exact := route.Exact()
			name, immediate := key.ImmediateChildName(entry)
			if !exact || !immediate {
				return result, failure(spec.Target(), ProtectionRegionIndeterminate, spec.RootRelativePath(), fmt.Errorf("native document name cannot be projected into its root key space"))
			}
			canonical = append(canonical, name)
		}
		specs, err = configuration.KnownRootDocuments(selector.reference, canonical)
		if err != nil {
			return physicalDocumentFacts{}, err
		}
	}
	for _, spec := range specs {
		if err := ctx.Err(); err != nil {
			return physicalDocumentFacts{}, err
		}
		endpoint, err := observeProtectionPath(filepath.Join(root.path, filepath.FromSlash(spec.RootRelativePath())), observer.budget)
		if err != nil {
			return result, failure(spec.Target(), ProtectionEndpointUnavailable, spec.RootRelativePath(), err)
		}
		for _, alias := range aliases {
			relation := documentRoleRelation(endpoint, alias.observation)
			if relation == protectionIndeterminate {
				return result, failure(spec.Target(), ProtectionRegionIndeterminate, spec.RootRelativePath(), fmt.Errorf("native document role is normalization-sensitive"))
			}
			if relation != protectionOverlap {
				continue
			}
			result.roles = append(result.roles, spec)
			if spec.Scope() == target.ScopeGlobal {
				result.global = append(result.global, selector.reference)
			}
		}
	}
	return result, nil
}
