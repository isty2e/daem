package namespace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	journaldocument "github.com/isty2e/daem/internal/effect/journal/document"

	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

var ErrNoTrustedWriterRoot = errors.New("native document referent has no explicitly supplied trusted writer root")

// RootSelection borrows a caller-selected root and its canonical document base.
// Persisted paths cannot create or enlarge this grant.
type RootSelection struct {
	SelectedPath string
	Root         *rootedpath.CapturedRoot
	creation     *nativeRootCreation
	literal      string
	endpoint     *retainedEndpoint
}

func (selection RootSelection) validate() error {
	if selection.capturedRoot() == nil || !filepath.IsAbs(selection.SelectedPath) || filepath.Clean(selection.SelectedPath) != selection.SelectedPath || strings.ContainsRune(selection.SelectedPath, '\x00') {
		return fmt.Errorf("selected root requires a retained handle and canonical lexical selector")
	}
	if selection.endpoint != nil && (selection.Root != nil || filepath.Dir(selection.endpoint.physical) != selection.SelectedPath) {
		return fmt.Errorf("selected endpoint witness differs from its parent")
	}
	if selection.endpoint != nil {
		return selection.endpoint.validate()
	}
	return nil
}

func (selection RootSelection) pointAlternative() (configuration.PointAlternative, bool) {
	if selection.endpoint == nil {
		return "", false
	}
	return selection.endpoint.alternative, true
}

func (selection RootSelection) capturedRoot() *rootedpath.CapturedRoot {
	if selection.endpoint != nil {
		return selection.endpoint.witness
	}
	return selection.Root
}

func (selection RootSelection) Close() error {
	if root := selection.capturedRoot(); root != nil {
		return root.Close()
	}
	return nil
}

// Roots supplies an immutable selection of borrowed roots. Missing unused
// bindings are allowed; every supplied selection must qualify before use.
type Roots struct {
	selected map[output.RootReference]RootSelection
}

func (roots Roots) With(reference output.RootReference, selection RootSelection) (Roots, error) {
	if err := reference.Validate(); err != nil {
		return Roots{}, err
	}
	if err := selection.validate(); err != nil {
		return Roots{}, err
	}
	if reference.Identity() == configuration.ProjectRootIdentity && selection.endpoint != nil {
		return Roots{}, fmt.Errorf("native manifest root requires a directory selection")
	}
	for existing := range roots.selected {
		if existing.Identity() == reference.Identity() && existing != reference {
			return Roots{}, fmt.Errorf("selected root identity %q has conflicting locator contracts", reference.Identity())
		}
	}
	result := roots
	result.selected = maps.Clone(roots.selected)
	if result.selected == nil {
		result.selected = make(map[output.RootReference]RootSelection)
	}
	result.selected[reference] = selection
	return result, nil
}

func (roots Roots) Selection(reference output.RootReference) (RootSelection, bool) {
	selection, supplied := roots.selected[reference]
	return selection, supplied
}

func (roots Roots) Manifest() (RootSelection, bool) {
	for reference, selection := range roots.selected {
		if reference.Identity() == configuration.ProjectRootIdentity {
			return selection, true
		}
	}
	return RootSelection{}, false
}

type RootGrant struct {
	reference     output.RootReference
	selection     RootSelection
	authority     rootedpath.Authority
	provenance    rootedpath.AuthorityProvenance
	endpoint      mutation.DirectoryEntryAuthorityObservation
	endpointRoute string
}

type QualifiedRoots struct {
	grants map[output.RootReference]RootGrant
}

func (roots QualifiedRoots) Selection(reference output.RootReference) (RootGrant, bool) {
	grant, present := roots.grants[reference]
	return grant, present
}

func (roots QualifiedRoots) Manifest() (RootGrant, bool) {
	for reference, grant := range roots.grants {
		if reference.Identity() == configuration.ProjectRootIdentity {
			return grant, true
		}
	}
	return RootGrant{}, false
}

func (grant RootGrant) Provenance() rootedpath.AuthorityProvenance { return grant.provenance }
func (grant RootGrant) SelectedPath() string                       { return grant.selection.SelectedPath }
func (grant RootGrant) RootReference() output.RootReference        { return grant.reference }

func (grant RootGrant) PointAlternative() (configuration.PointAlternative, bool) {
	return grant.selection.pointAlternative()
}

func (grant RootGrant) ValidateSelection(selector string, budget rootedpath.PhysicalTraversalBudget) error {
	if grant.selection.capturedRoot() == nil {
		return fmt.Errorf("native root grant is required")
	}
	return grant.selection.capturedRoot().ValidateSelectionBounded(selector, physicalwork.MaximumPhysicalPathDepth, budget)
}

type Alias struct {
	address     aggregate.DocumentAddress
	location    configuration.AliasLocation
	binding     configuration.AliasBinding
	root        RootGrant
	relative    rootedpath.RelativeDestination
	route       string
	observation mutation.DirectoryEntryAuthorityObservation
}

func (alias Alias) Address() aggregate.DocumentAddress { return alias.address }
func (alias Alias) Observation() mutation.DirectoryEntryAuthorityObservation {
	return alias.observation
}
func (alias Alias) Root() RootGrant                       { return alias.root }
func (alias Alias) Location() configuration.AliasLocation { return alias.location }

// Binding is unavailable when the recorded format did not retain a document policy.
func (alias Alias) Binding() (configuration.AliasBinding, bool) {
	return alias.binding, alias.binding != (configuration.AliasBinding{})
}

// WriterPin is durable identity evidence, not a current route or
// permission to acquire outside independently selected roots.
// Its value validation participates in persisted native-journal interpretation;
// changes require compatibility review, not just live eligibility review.
type WriterPin struct {
	reference  output.RootReference
	provenance rootedpath.AuthorityProvenance
	coordinate configuration.RootCoordinate
}

func NewWriterPin(provenance rootedpath.AuthorityProvenance, relative rootedpath.RelativeDestination) (WriterPin, error) {
	if err := relative.Validate(); err != nil {
		return WriterPin{}, err
	}
	coordinate, err := configuration.NewDirectoryChild(relative.Path())
	if err != nil {
		return WriterPin{}, err
	}
	pin := WriterPin{provenance: provenance, coordinate: coordinate}
	if err := pin.Validate(); err != nil {
		return WriterPin{}, err
	}
	return pin, nil
}

func NewBoundWriterPin(reference output.RootReference, provenance rootedpath.AuthorityProvenance, coordinate configuration.RootCoordinate) (WriterPin, error) {
	if err := reference.Validate(); err != nil {
		return WriterPin{}, err
	}
	pin := WriterPin{reference: reference, provenance: provenance, coordinate: coordinate}
	if err := pin.Validate(); err != nil {
		return WriterPin{}, err
	}
	return pin, nil
}

func (pin WriterPin) Provenance() rootedpath.AuthorityProvenance { return pin.provenance }
func (pin WriterPin) Coordinate() configuration.RootCoordinate   { return pin.coordinate }
func (pin WriterPin) RootReference() (output.RootReference, bool) {
	return pin.reference, pin.reference != (output.RootReference{})
}

func (pin WriterPin) Validate() error {
	if err := pin.provenance.Validate(); err != nil {
		return err
	}
	if err := pin.coordinate.Validate(); err != nil {
		return err
	}
	if child, directory := pin.coordinate.DirectoryChild(); directory {
		if _, err := rootedpath.NewRelativeDestination(child); err != nil {
			return err
		}
	}
	if reference, bound := pin.RootReference(); bound {
		return reference.Validate()
	}
	if pin.coordinate.Kind() != configuration.CoordinateDirectoryChild {
		return fmt.Errorf("unbound writer pin requires a directory-child coordinate")
	}
	return nil
}

// DocumentNamespace binds real aliases to one rooted referent. It is
// namespace evidence, not ownership, selected-value or operation authority.
type DocumentNamespace struct {
	roots       Roots
	protection  ReservationSelectors
	physical    physicalDocumentFacts
	filesystem  mutationfs.RootedReader
	inputs      []aliasInput
	aliases     []Alias
	observation mutation.DirectoryEntryAuthorityObservation
	writerRoot  *rootedpath.CapturedRoot
	writer      rootedpath.Destination
	writerPin   WriterPin
}

// ObserveAlias returns grouping metadata after binding/root qualification. It
// neither checks resource protection nor supplies a readable/writable namespace.
func ObserveAlias(ctx context.Context, address aggregate.DocumentAddress, roots Roots, budget rootedpath.PhysicalTraversalBudget) (mutation.DirectoryEntryAuthorityObservation, error) {
	if ctx == nil || budget == nil {
		return mutation.DirectoryEntryAuthorityObservation{}, fmt.Errorf("native alias observation requires context and shared traversal budget")
	}
	inputs, err := currentAliasInputs(ctx, []aggregate.DocumentAddress{address})
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	qualified, err := roots.Qualify(ctx, budget)
	if err != nil {
		return mutation.DirectoryEntryAuthorityObservation{}, err
	}
	alias, _, err := observeNativeAlias(ctx, inputs[0], qualified.grants, budget)
	return alias.observation, err
}

func Observe(ctx context.Context, aliases []aggregate.DocumentAddress, roots Roots, protection ReservationSelectors, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget) (DocumentNamespace, error) {
	if ctx == nil || filesystem == nil || budget == nil {
		return DocumentNamespace{}, fmt.Errorf("native namespace context, filesystem and shared traversal budget are required")
	}
	inputs, err := currentAliasInputs(ctx, aliases)
	if err != nil {
		return DocumentNamespace{}, err
	}
	return observeNativeDocumentNamespace(ctx, inputs, roots, protection, filesystem, budget, nil)
}

func Reobserve(ctx context.Context, aliases []RecordedAlias, roots Roots, protection ReservationSelectors, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget, pin WriterPin) (DocumentNamespace, error) {
	if err := pin.Validate(); err != nil {
		return DocumentNamespace{}, err
	}
	if ctx == nil || filesystem == nil || budget == nil {
		return DocumentNamespace{}, fmt.Errorf("native namespace context, filesystem and shared traversal budget are required")
	}
	if err := ctx.Err(); err != nil {
		return DocumentNamespace{}, err
	}
	inputs, err := recordedAliasInputs(ctx, aliases)
	if err != nil {
		return DocumentNamespace{}, err
	}
	return observeNativeDocumentNamespace(ctx, inputs, roots, protection, filesystem, budget, &pin)
}

func (namespace DocumentNamespace) Observation() mutation.DirectoryEntryAuthorityObservation {
	return namespace.observation
}

func (namespace DocumentNamespace) Addresses() []aggregate.DocumentAddress {
	addresses := make([]aggregate.DocumentAddress, 0, len(namespace.aliases))
	for _, alias := range namespace.aliases {
		addresses = append(addresses, alias.address)
	}
	return addresses
}

func (namespace DocumentNamespace) Aliases() []Alias     { return slices.Clone(namespace.aliases) }
func (namespace DocumentNamespace) WriterPin() WriterPin { return namespace.writerPin }
func (namespace DocumentNamespace) WriterPath() (string, error) {
	return namespace.writer.LexicalPath()
}

func (namespace DocumentNamespace) QualifyRoots(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (QualifiedRoots, error) {
	if err := namespace.observation.Validate(); err != nil {
		return QualifiedRoots{}, err
	}
	return namespace.roots.Qualify(ctx, budget)
}

// AcquireReferent requalifies root selections and aliases before returning a
// final-entry-no-follow capability. The caller closes or consumes the capability.
func (namespace DocumentNamespace) AcquireReferent(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, error) {
	current, err := observeNativeDocumentNamespace(ctx, namespace.inputs, namespace.roots, namespace.protection, namespace.filesystem, budget, &namespace.writerPin)
	if err != nil {
		return nil, err
	}
	if !namespace.observation.Equal(current.observation) || !namespace.physical.equal(current.physical) {
		return nil, fmt.Errorf("native document namespace changed before referent acquisition")
	}
	capability, err := current.writerRoot.AcquireBounded(current.writer, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, capability.Close())
	}
	return capability, nil
}

func observeNativeDocumentNamespace(ctx context.Context, inputs []aliasInput, roots Roots, protection ReservationSelectors, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget, pinned *WriterPin) (DocumentNamespace, error) {
	if ctx == nil || filesystem == nil || budget == nil {
		return DocumentNamespace{}, fmt.Errorf("native namespace context, filesystem and shared traversal budget are required")
	}
	if err := ctx.Err(); err != nil {
		return DocumentNamespace{}, err
	}
	if err := validateAliasInputs(ctx, inputs); err != nil {
		return DocumentNamespace{}, err
	}
	qualified, err := roots.Qualify(ctx, budget)
	if err != nil {
		return DocumentNamespace{}, err
	}
	aliases := slices.Clone(inputs)
	slices.SortFunc(aliases, func(left, right aliasInput) int {
		return journaldocument.CompareAliases(left.address, right.address)
	})
	grants := qualified.grants
	result := DocumentNamespace{roots: roots, protection: protection, filesystem: filesystem, inputs: aliases}
	boundRoots := make(map[output.RootReference]struct{})
	var referent string
	for _, input := range aliases {
		alias, physical, err := observeNativeAlias(ctx, input, grants, budget)
		if err != nil {
			return DocumentNamespace{}, err
		}
		if len(result.aliases) == 0 {
			result.observation, referent = alias.observation, physical
		} else if !result.observation.Equal(alias.observation) {
			return DocumentNamespace{}, fmt.Errorf("native document aliases do not identify one physical referent")
		}
		result.aliases = append(result.aliases, alias)
		boundRoots[alias.location.RootReference()] = struct{}{}
	}
	observer := newPhysicalObserver(protection, budget)
	if err := observer.requireUnreserved(ctx, result.aliases); err != nil {
		return DocumentNamespace{}, err
	}
	result.physical, err = observer.documents(ctx, result.aliases, qualified)
	if err != nil {
		return DocumentNamespace{}, err
	}
	writerGrant, relative, err := nativeDocumentWriterGrant(grants, boundRoots, referent, result.observation, pinned)
	if err != nil {
		return DocumentNamespace{}, err
	}
	writer, err := writerGrant.authority.Bind(relative)
	if err != nil {
		return DocumentNamespace{}, err
	}
	writerPath, err := writer.LexicalPath()
	if err != nil {
		return DocumentNamespace{}, err
	}
	if writerGrant.selection.creation != nil && !nativeRootContains(writerGrant.selection.creation.physical, writerPath) {
		return DocumentNamespace{}, fmt.Errorf("native recorded writer escapes the selected creation prefix")
	}
	observedWriter, err := mutation.ObserveDirectoryEntryAuthorityBounded(writerPath, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return DocumentNamespace{}, err
	}
	if !result.observation.Equal(observedWriter) {
		return DocumentNamespace{}, fmt.Errorf("native document writer does not identify the aliased referent")
	}
	capability, err := writerGrant.selection.capturedRoot().AcquireBounded(writer, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return DocumentNamespace{}, err
	}
	identity, observeErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if observeErr != nil && !errors.Is(observeErr, fs.ErrNotExist) {
		return DocumentNamespace{}, errors.Join(observeErr, closeErr)
	}
	if closeErr != nil {
		return DocumentNamespace{}, closeErr
	}
	if observeErr == nil && (identity == nil || identity.Kind() != mutationfs.EntryKindFile) {
		return DocumentNamespace{}, fmt.Errorf("native document referent is not a regular file or absent entry")
	}
	if err := ctx.Err(); err != nil {
		return DocumentNamespace{}, err
	}
	result.writerRoot, result.writer = writerGrant.selection.capturedRoot(), writer
	if pinned != nil && pinned.reference == (output.RootReference{}) {
		result.writerPin, err = NewWriterPin(writerGrant.provenance, relative)
	} else {
		coordinate := configuration.PointSelf()
		if writerGrant.selection.endpoint == nil {
			coordinate, err = configuration.NewDirectoryChild(relative.Path())
			if err != nil {
				return DocumentNamespace{}, err
			}
		}
		result.writerPin, err = NewBoundWriterPin(writerGrant.reference, writerGrant.provenance, coordinate)
	}
	if err != nil {
		return DocumentNamespace{}, err
	}
	return result, nil
}

func observeNativeAlias(ctx context.Context, input aliasInput, grants map[output.RootReference]RootGrant, budget rootedpath.PhysicalTraversalBudget) (Alias, string, error) {
	if err := ctx.Err(); err != nil {
		return Alias{}, "", err
	}
	address := input.address
	if err := validateNativeDocumentRootRole(address, address.AggregateRoot().RootRole()); err != nil {
		return Alias{}, "", err
	}
	location := input.location()
	grant, present := grants[location.RootReference()]
	if !present {
		return Alias{}, "", fmt.Errorf("native document alias has no independently selected root")
	}
	coordinate := location.Coordinate()
	point := coordinate.Kind() == configuration.CoordinatePointSelf
	if point != (grant.selection.endpoint != nil) {
		return Alias{}, "", fmt.Errorf("native alias coordinate and selected root kind differ")
	}
	if point {
		return Alias{
			address: address, location: location, binding: input.binding, root: grant,
			route: grant.endpointRoute, observation: grant.endpoint,
		}, grant.selection.endpoint.physical, nil
	}
	child, directory := coordinate.DirectoryChild()
	if !directory {
		return Alias{}, "", fmt.Errorf("native alias directory coordinate is required")
	}
	selectedRelative, err := rootedpath.NewRelativeDestination(child)
	if err != nil {
		return Alias{}, "", err
	}

	logical := filepath.Join(grant.selection.SelectedPath, filepath.FromSlash(selectedRelative.Path()))
	physical, err := rootedpath.ResolveReferentPathBounded(logical, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return Alias{}, "", err
	}
	observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return Alias{}, "", err
	}
	if err := ctx.Err(); err != nil {
		return Alias{}, "", err
	}
	return Alias{address: address, location: location, binding: input.binding, root: grant, relative: selectedRelative, route: logical, observation: observation}, physical, nil
}

func (roots Roots) Qualify(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (QualifiedRoots, error) {
	if ctx == nil || budget == nil {
		return QualifiedRoots{}, fmt.Errorf("native roots require context and shared traversal budget")
	}
	if err := ctx.Err(); err != nil {
		return QualifiedRoots{}, err
	}
	references := slices.Collect(maps.Keys(roots.selected))
	slices.SortFunc(references, output.RootReference.Compare)
	result := make(map[output.RootReference]RootGrant, len(roots.selected))
	for _, reference := range references {
		if err := ctx.Err(); err != nil {
			return QualifiedRoots{}, err
		}
		if err := reference.Validate(); err != nil {
			return QualifiedRoots{}, err
		}
		selection := roots.selected[reference]
		if err := selection.validate(); err != nil {
			return QualifiedRoots{}, err
		}
		var endpointRoute string
		var err error
		if selection.endpoint != nil {
			endpointRoute, err = selection.endpoint.qualify(ctx, budget)
		} else {
			err = selection.qualifyLocator(ctx, budget)
		}
		if err != nil {
			return QualifiedRoots{}, err
		}
		if selection.creation != nil {
			if reference.Identity() == configuration.ProjectRootIdentity {
				return QualifiedRoots{}, fmt.Errorf("native manifest root must already exist")
			}
			if _, err := selection.qualifyCreation(ctx, budget); err != nil {
				return QualifiedRoots{}, err
			}
		} else if err := selection.capturedRoot().ValidateSelectionBounded(selection.SelectedPath, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
			return QualifiedRoots{}, err
		}
		authority, err := selection.capturedRoot().AuthorityBounded(budget)
		if err != nil {
			return QualifiedRoots{}, err
		}
		provenance, err := authority.Provenance()
		if err != nil {
			return QualifiedRoots{}, err
		}
		grant := RootGrant{reference: reference, selection: selection, authority: authority, provenance: provenance, endpointRoute: endpointRoute}
		if selection.endpoint != nil {
			grant.endpoint, err = mutation.ObserveDirectoryEntryAuthorityBounded(selection.endpoint.physical, physicalwork.MaximumPhysicalPathDepth, budget)
			if err != nil {
				return QualifiedRoots{}, err
			}
		}
		result[reference] = grant
	}
	return QualifiedRoots{grants: result}, nil
}

func validateNativeDocumentRootRole(address aggregate.DocumentAddress, role output.RootRole) error {
	if address.Target() != target.TargetPi && address.Target() != target.TargetCodex {
		return fmt.Errorf("native document namespace requires a Pi or Codex alias")
	}
	if address.Scope() == target.ScopeProject && role == output.RootProject {
		return nil
	}
	if address.Scope() == target.ScopeGlobal && (address.Target() == target.TargetPi && role == output.RootPiAgent || address.Target() == target.TargetCodex && role == output.RootCodexHome) {
		return nil
	}
	return fmt.Errorf("native document alias contradicts its selected host root role")
}

func nativeDocumentWriterGrant(grants map[output.RootReference]RootGrant, boundRoots map[output.RootReference]struct{}, referent string, observation mutation.DirectoryEntryAuthorityObservation, pinned *WriterPin) (RootGrant, rootedpath.RelativeDestination, error) {
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		return RootGrant{}, rootedpath.RelativeDestination{}, err
	}
	unboundEndpoints := make(map[output.RootReference]struct{})
	for _, contract := range contracts {
		if contract.Envelope() == configuration.NativeEndpoints {
			if _, bound := boundRoots[contract.Reference()]; !bound {
				unboundEndpoints[contract.Reference()] = struct{}{}
			}
		}
	}

	var selected RootGrant
	var relative rootedpath.RelativeDestination
	references := slices.Collect(maps.Keys(grants))
	slices.SortFunc(references, output.RootReference.Compare)
	for _, reference := range references {
		if _, unbound := unboundEndpoints[reference]; unbound {
			continue
		}
		grant := grants[reference]
		writerReferent := referent
		if grant.selection.endpoint != nil {
			if _, bound := boundRoots[reference]; !bound || !observation.Equal(grant.endpoint) {
				continue
			}
			writerReferent = grant.selection.endpoint.physical
		}
		if grant.selection.creation != nil && !nativeRootContains(grant.selection.creation.physical, referent) {
			continue
		}
		path, err := filepath.Rel(grant.authority.PhysicalRoot(), writerReferent)
		if err != nil {
			continue
		}
		candidate, err := rootedpath.NewRelativeDestination(filepath.ToSlash(path))
		if err != nil {
			continue
		}
		if pinned != nil {
			matched, selected, err := pinned.matchGrant(grant, candidate)
			if err != nil {
				return RootGrant{}, rootedpath.RelativeDestination{}, err
			}
			if !matched {
				continue
			}
			return grant, selected, nil
		}
		if selected.selection.capturedRoot() == nil || len(grant.authority.PhysicalRoot()) > len(selected.authority.PhysicalRoot()) {
			selected, relative = grant, candidate
		}
	}
	if selected.selection.capturedRoot() == nil {
		return RootGrant{}, rootedpath.RelativeDestination{}, ErrNoTrustedWriterRoot
	}
	return selected, relative, nil
}

func (pin WriterPin) matchGrant(grant RootGrant, derived rootedpath.RelativeDestination) (bool, rootedpath.RelativeDestination, error) {
	reference, bound := pin.RootReference()
	if bound && reference != grant.reference || pin.provenance != grant.provenance {
		return false, rootedpath.RelativeDestination{}, nil
	}
	point := grant.selection.endpoint != nil
	if point != (pin.coordinate.Kind() == configuration.CoordinatePointSelf) || point && !bound {
		return false, rootedpath.RelativeDestination{}, nil
	}
	if point {
		return true, derived, nil
	}
	child, _ := pin.coordinate.DirectoryChild()
	relative, err := rootedpath.NewRelativeDestination(child)
	return err == nil, relative, err
}
