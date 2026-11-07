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

// RootSelection borrows a caller-selected root and its lexical selector.
// Persisted paths cannot create or enlarge this grant.
type RootSelection struct {
	SelectedPath string
	Root         *rootedpath.CapturedRoot
	creation     *nativeRootCreation
}

func (selection RootSelection) validate() error {
	if selection.Root == nil || !filepath.IsAbs(selection.SelectedPath) || filepath.Clean(selection.SelectedPath) != selection.SelectedPath || strings.ContainsRune(selection.SelectedPath, '\x00') {
		return fmt.Errorf("selected root requires a retained handle and canonical lexical selector")
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
	reference, err := configuration.ProjectRootReference()
	if err != nil {
		return RootSelection{}, false
	}
	return roots.Selection(reference)
}

type RootGrant struct {
	selection  RootSelection
	authority  rootedpath.Authority
	provenance rootedpath.AuthorityProvenance
}

type QualifiedRoots struct {
	grants map[output.RootReference]RootGrant
}

func (roots QualifiedRoots) Manifest() (RootGrant, bool) {
	reference, err := configuration.ProjectRootReference()
	if err != nil {
		return RootGrant{}, false
	}
	grant, present := roots.grants[reference]
	return grant, present
}

func (grant RootGrant) Provenance() rootedpath.AuthorityProvenance { return grant.provenance }
func (grant RootGrant) SelectedPath() string                       { return grant.selection.SelectedPath }

func (grant RootGrant) ValidateSelection(selector string, budget rootedpath.PhysicalTraversalBudget) error {
	if grant.selection.Root == nil {
		return fmt.Errorf("native root grant is required")
	}
	return grant.selection.Root.ValidateSelectionBounded(selector, physicalwork.MaximumPhysicalPathDepth, budget)
}

type Alias struct {
	address     aggregate.DocumentAddress
	root        RootGrant
	relative    rootedpath.RelativeDestination
	observation mutation.DirectoryEntryAuthorityObservation
}

func (alias Alias) Address() aggregate.DocumentAddress { return alias.address }
func (alias Alias) Observation() mutation.DirectoryEntryAuthorityObservation {
	return alias.observation
}
func (alias Alias) Root() RootGrant { return alias.root }

// WriterPin is durable identity evidence, not a current route or
// permission to acquire outside independently selected roots.
type WriterPin struct {
	provenance rootedpath.AuthorityProvenance
	relative   rootedpath.RelativeDestination
}

func NewWriterPin(provenance rootedpath.AuthorityProvenance, relative rootedpath.RelativeDestination) (WriterPin, error) {
	if err := provenance.Validate(); err != nil {
		return WriterPin{}, err
	}
	if err := relative.Validate(); err != nil {
		return WriterPin{}, err
	}
	return WriterPin{provenance: provenance, relative: relative}, nil
}

func (pin WriterPin) Provenance() rootedpath.AuthorityProvenance { return pin.provenance }
func (pin WriterPin) Relative() rootedpath.RelativeDestination   { return pin.relative }

// DocumentNamespace binds real aliases to one rooted referent. It is
// namespace evidence, not ownership, selected-value or operation authority.
type DocumentNamespace struct {
	roots       Roots
	filesystem  mutationfs.RootedReader
	aliases     []Alias
	observation mutation.DirectoryEntryAuthorityObservation
	writerRoot  *rootedpath.CapturedRoot
	writer      rootedpath.Destination
	writerPin   WriterPin
}

func Observe(ctx context.Context, aliases []aggregate.DocumentAddress, roots Roots, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget) (DocumentNamespace, error) {
	return observeNativeDocumentNamespace(ctx, aliases, roots, filesystem, budget, nil)
}

func Reobserve(ctx context.Context, aliases []aggregate.DocumentAddress, roots Roots, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget, pin WriterPin) (DocumentNamespace, error) {
	if _, err := NewWriterPin(pin.provenance, pin.relative); err != nil {
		return DocumentNamespace{}, err
	}
	return observeNativeDocumentNamespace(ctx, aliases, roots, filesystem, budget, &pin)
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
	current, err := observeNativeDocumentNamespace(ctx, namespace.Addresses(), namespace.roots, namespace.filesystem, budget, &namespace.writerPin)
	if err != nil {
		return nil, err
	}
	if !namespace.observation.Equal(current.observation) {
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

func observeNativeDocumentNamespace(ctx context.Context, inputs []aggregate.DocumentAddress, roots Roots, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget, pinned *WriterPin) (DocumentNamespace, error) {
	if ctx == nil || filesystem == nil || budget == nil {
		return DocumentNamespace{}, fmt.Errorf("native namespace context, filesystem and shared traversal budget are required")
	}
	if err := ctx.Err(); err != nil {
		return DocumentNamespace{}, err
	}
	if _, err := journaldocument.AliasSet(ctx, inputs); err != nil {
		return DocumentNamespace{}, err
	}
	qualified, err := roots.Qualify(ctx, budget)
	if err != nil {
		return DocumentNamespace{}, err
	}
	aliases := slices.Clone(inputs)
	slices.SortFunc(aliases, journaldocument.CompareAliases)
	grants := qualified.grants
	result := DocumentNamespace{roots: roots, filesystem: filesystem}
	var referent string
	for _, address := range aliases {
		if err := ctx.Err(); err != nil {
			return DocumentNamespace{}, err
		}
		role := address.AggregateRoot().RootRole()
		if err := validateNativeDocumentRootRole(address, role); err != nil {
			return DocumentNamespace{}, err
		}
		reference, relative, err := configuration.RootBindingForAlias(configuration.AliasInput{Target: address.Target(), Scope: address.Scope(), Destination: address.AggregateRoot()})
		if err != nil {
			return DocumentNamespace{}, err
		}
		selectedRelative, err := rootedpath.NewRelativeDestination(relative)
		if err != nil {
			return DocumentNamespace{}, err
		}
		grant, present := grants[reference]
		if !present {
			return DocumentNamespace{}, fmt.Errorf("native document alias has no independently selected root")
		}
		logical := filepath.Join(grant.selection.SelectedPath, filepath.FromSlash(selectedRelative.Path()))
		physical, err := rootedpath.ResolveReferentPathBounded(logical, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return DocumentNamespace{}, err
		}
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, physicalwork.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return DocumentNamespace{}, err
		}
		if len(result.aliases) == 0 {
			result.observation, referent = observation, physical
		} else if !result.observation.Equal(observation) {
			return DocumentNamespace{}, fmt.Errorf("native document aliases do not identify one physical referent")
		}
		result.aliases = append(result.aliases, Alias{address: address, root: grant, relative: selectedRelative, observation: observation})
	}
	writerGrant, relative, err := nativeDocumentWriterGrant(grants, referent, pinned)
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
	capability, err := writerGrant.selection.Root.AcquireBounded(writer, physicalwork.MaximumPhysicalPathDepth, budget)
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
	result.writerRoot, result.writer = writerGrant.selection.Root, writer
	result.writerPin, err = NewWriterPin(writerGrant.provenance, relative)
	if err != nil {
		return DocumentNamespace{}, err
	}
	return result, nil
}

func (roots Roots) Qualify(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (QualifiedRoots, error) {
	if ctx == nil || budget == nil {
		return QualifiedRoots{}, fmt.Errorf("native roots require context and shared traversal budget")
	}
	if err := ctx.Err(); err != nil {
		return QualifiedRoots{}, err
	}
	manifest, err := configuration.ProjectRootReference()
	if err != nil {
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
		if selection.creation != nil {
			if reference == manifest {
				return QualifiedRoots{}, fmt.Errorf("native manifest root must already exist")
			}
			if _, err := selection.qualifyCreation(ctx, budget); err != nil {
				return QualifiedRoots{}, err
			}
		} else if err := selection.Root.ValidateSelectionBounded(selection.SelectedPath, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
			return QualifiedRoots{}, err
		}
		authority, err := selection.Root.AuthorityBounded(budget)
		if err != nil {
			return QualifiedRoots{}, err
		}
		provenance, err := authority.Provenance()
		if err != nil {
			return QualifiedRoots{}, err
		}
		result[reference] = RootGrant{selection, authority, provenance}
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

func nativeDocumentWriterGrant(grants map[output.RootReference]RootGrant, referent string, pinned *WriterPin) (RootGrant, rootedpath.RelativeDestination, error) {
	var selected RootGrant
	var relative rootedpath.RelativeDestination
	references := slices.Collect(maps.Keys(grants))
	slices.SortFunc(references, output.RootReference.Compare)
	for _, reference := range references {
		grant := grants[reference]
		if grant.selection.creation != nil && !nativeRootContains(grant.selection.creation.physical, referent) {
			continue
		}
		path, err := filepath.Rel(grant.authority.PhysicalRoot(), referent)
		if err != nil {
			continue
		}
		candidate, err := rootedpath.NewRelativeDestination(filepath.ToSlash(path))
		if err != nil {
			continue
		}
		if pinned != nil {
			if grant.provenance != pinned.provenance {
				continue
			}
			candidate = pinned.relative
			return grant, candidate, nil
		}
		if selected.selection.Root == nil || len(grant.authority.PhysicalRoot()) > len(selected.authority.PhysicalRoot()) {
			selected, relative = grant, candidate
		}
	}
	if selected.selection.Root == nil {
		return RootGrant{}, rootedpath.RelativeDestination{}, fmt.Errorf("native document referent has no explicitly supplied trusted writer root")
	}
	return selected, relative, nil
}
