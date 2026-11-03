package journal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// NativeDocumentRoot borrows a caller-selected root and its lexical selector.
// Persisted paths cannot create or enlarge this grant.
type NativeDocumentRoot struct {
	SelectedPath string
	Root         *rootedpath.CapturedRoot
	creation     *nativeRootCreation
}

// NativeDocumentRoots supplies only the independently selected roots available
// to this operation. Missing unused roles are allowed; supplied grants must qualify.
type NativeDocumentRoots struct {
	Manifest  NativeDocumentRoot
	PiAgent   NativeDocumentRoot
	CodexHome NativeDocumentRoot
}

type nativeDocumentRootGrant struct {
	selection  NativeDocumentRoot
	authority  rootedpath.Authority
	provenance rootedpath.AuthorityProvenance
}

type nativeDocumentAliasObservation struct {
	address     aggregate.DocumentAddress
	root        nativeDocumentRootGrant
	observation mutation.DirectoryEntryAuthorityObservation
}

// NativeDocumentNamespace binds real aliases to one rooted referent. It is
// namespace evidence, not ownership, selected-value or operation authority.
type NativeDocumentNamespace struct {
	roots        NativeDocumentRoots
	filesystem   mutationfs.RootedReader
	aliases      []nativeDocumentAliasObservation
	observation  mutation.DirectoryEntryAuthorityObservation
	writerRoot   *rootedpath.CapturedRoot
	writer       rootedpath.Destination
	writerRecord recoveryDocumentWriter
}

func ObserveNativeDocumentNamespace(ctx context.Context, aliases []aggregate.DocumentAddress, roots NativeDocumentRoots, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget) (NativeDocumentNamespace, error) {
	return observeNativeDocumentNamespace(ctx, aliases, roots, filesystem, budget, nil)
}

func (namespace NativeDocumentNamespace) Observation() mutation.DirectoryEntryAuthorityObservation {
	return namespace.observation
}

func (namespace NativeDocumentNamespace) Addresses() []aggregate.DocumentAddress {
	addresses := make([]aggregate.DocumentAddress, 0, len(namespace.aliases))
	for _, alias := range namespace.aliases {
		addresses = append(addresses, alias.address)
	}
	return addresses
}

// AcquireReferent requalifies root selections and aliases before returning a
// final-entry-no-follow capability. The caller closes or consumes the capability.
func (namespace NativeDocumentNamespace) AcquireReferent(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (rootedpath.CommitCapability, error) {
	current, err := observeNativeDocumentNamespace(ctx, namespace.Addresses(), namespace.roots, namespace.filesystem, budget, &namespace.writerRecord)
	if err != nil {
		return nil, err
	}
	if !namespace.observation.Equal(current.observation) {
		return nil, fmt.Errorf("native document namespace changed before referent acquisition")
	}
	capability, err := current.writerRoot.AcquireBounded(current.writer, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, capability.Close())
	}
	return capability, nil
}

func observeNativeDocumentNamespace(ctx context.Context, inputs []aggregate.DocumentAddress, roots NativeDocumentRoots, filesystem mutationfs.RootedReader, budget rootedpath.PhysicalTraversalBudget, pinned *recoveryDocumentWriter) (NativeDocumentNamespace, error) {
	if ctx == nil || filesystem == nil || budget == nil {
		return NativeDocumentNamespace{}, fmt.Errorf("native namespace context, filesystem and shared traversal budget are required")
	}
	if err := ctx.Err(); err != nil {
		return NativeDocumentNamespace{}, err
	}
	if _, err := documentAliasSet(ctx, inputs); err != nil {
		return NativeDocumentNamespace{}, err
	}
	grants, err := roots.qualify(ctx, budget)
	if err != nil {
		return NativeDocumentNamespace{}, err
	}
	aliases := slices.Clone(inputs)
	slices.SortFunc(aliases, func(left, right aggregate.DocumentAddress) int {
		return compareDocumentAliasRefs(persistedDocumentAlias(left), persistedDocumentAlias(right))
	})
	result := NativeDocumentNamespace{roots: roots, filesystem: filesystem}
	var referent string
	for _, address := range aliases {
		if err := ctx.Err(); err != nil {
			return NativeDocumentNamespace{}, err
		}
		role := address.AggregateRoot().RootRole()
		if err := validateNativeDocumentRootRole(address, role); err != nil {
			return NativeDocumentNamespace{}, err
		}
		grant, present := grants[role]
		if !present {
			return NativeDocumentNamespace{}, fmt.Errorf("native document alias has no independently selected root")
		}
		logical := filepath.Join(grant.selection.SelectedPath, filepath.FromSlash(address.AggregateRoot().RelativePath()))
		physical, err := rootedpath.ResolveReferentPathBounded(logical, recovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return NativeDocumentNamespace{}, err
		}
		observation, err := mutation.ObserveDirectoryEntryAuthorityBounded(physical, recovery.MaximumPhysicalPathDepth, budget)
		if err != nil {
			return NativeDocumentNamespace{}, err
		}
		if len(result.aliases) == 0 {
			result.observation, referent = observation, physical
		} else if !result.observation.Equal(observation) {
			return NativeDocumentNamespace{}, fmt.Errorf("native document aliases do not identify one physical referent")
		}
		result.aliases = append(result.aliases, nativeDocumentAliasObservation{address: address, root: grant, observation: observation})
	}
	writerGrant, relative, err := nativeDocumentWriterGrant(grants, referent, pinned)
	if err != nil {
		return NativeDocumentNamespace{}, err
	}
	writer, err := writerGrant.authority.Bind(relative)
	if err != nil {
		return NativeDocumentNamespace{}, err
	}
	writerPath, err := writer.LexicalPath()
	if err != nil {
		return NativeDocumentNamespace{}, err
	}
	if writerGrant.selection.creation != nil && !nativeRootContains(writerGrant.selection.creation.physical, writerPath) {
		return NativeDocumentNamespace{}, fmt.Errorf("native recorded writer escapes the selected creation prefix")
	}
	observedWriter, err := mutation.ObserveDirectoryEntryAuthorityBounded(writerPath, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return NativeDocumentNamespace{}, err
	}
	if !result.observation.Equal(observedWriter) {
		return NativeDocumentNamespace{}, fmt.Errorf("native document writer does not identify the aliased referent")
	}
	capability, err := writerGrant.selection.Root.AcquireBounded(writer, recovery.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return NativeDocumentNamespace{}, err
	}
	identity, observeErr := filesystem.CaptureRootedEntryIdentity(ctx, capability)
	closeErr := capability.Close()
	if observeErr != nil && !errors.Is(observeErr, fs.ErrNotExist) {
		return NativeDocumentNamespace{}, errors.Join(observeErr, closeErr)
	}
	if closeErr != nil {
		return NativeDocumentNamespace{}, closeErr
	}
	if observeErr == nil && (identity == nil || identity.Kind() != mutationfs.EntryKindFile) {
		return NativeDocumentNamespace{}, fmt.Errorf("native document referent is not a regular file or absent entry")
	}
	if err := ctx.Err(); err != nil {
		return NativeDocumentNamespace{}, err
	}
	result.writerRoot, result.writer = writerGrant.selection.Root, writer
	result.writerRecord = recoveryDocumentWriter{Root: persistedNativeRoot(writerGrant.provenance), RelativePath: relative.Path()}
	return result, nil
}

func (roots NativeDocumentRoots) qualify(ctx context.Context, budget rootedpath.PhysicalTraversalBudget) (map[output.RootRole]nativeDocumentRootGrant, error) {
	result := make(map[output.RootRole]nativeDocumentRootGrant)
	for _, input := range []struct {
		role      output.RootRole
		selection NativeDocumentRoot
	}{{output.RootProject, roots.Manifest}, {output.RootPiAgent, roots.PiAgent}, {output.RootCodexHome, roots.CodexHome}} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		selection := input.selection
		if selection.Root == nil && selection.SelectedPath == "" {
			continue
		}
		if selection.Root == nil || !filepath.IsAbs(selection.SelectedPath) || filepath.Clean(selection.SelectedPath) != selection.SelectedPath || strings.ContainsRune(selection.SelectedPath, '\x00') {
			return nil, fmt.Errorf("native document root requires an actual retained root and canonical lexical selector")
		}
		if selection.creation != nil {
			if input.role == output.RootProject {
				return nil, fmt.Errorf("native manifest root must already exist")
			}
			if _, err := selection.qualifyCreation(ctx, budget); err != nil {
				return nil, err
			}
		} else if err := selection.Root.ValidateSelectionBounded(selection.SelectedPath, recovery.MaximumPhysicalPathDepth, budget); err != nil {
			return nil, err
		}
		authority, err := selection.Root.AuthorityBounded(budget)
		if err != nil {
			return nil, err
		}
		provenance, err := authority.Provenance()
		if err != nil {
			return nil, err
		}
		result[input.role] = nativeDocumentRootGrant{selection, authority, provenance}
	}
	return result, nil
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

func nativeDocumentWriterGrant(grants map[output.RootRole]nativeDocumentRootGrant, referent string, pinned *recoveryDocumentWriter) (nativeDocumentRootGrant, rootedpath.RelativeDestination, error) {
	var selected nativeDocumentRootGrant
	var relative rootedpath.RelativeDestination
	for _, role := range []output.RootRole{output.RootProject, output.RootPiAgent, output.RootCodexHome} {
		grant, supplied := grants[role]
		if !supplied {
			continue
		}
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
			if persistedNativeRoot(grant.provenance) != pinned.Root {
				continue
			}
			candidate, err = rootedpath.NewRelativeDestination(pinned.RelativePath)
			if err != nil || candidate.Path() != pinned.RelativePath {
				return nativeDocumentRootGrant{}, rootedpath.RelativeDestination{}, fmt.Errorf("recorded writer relative referent is invalid")
			}
			return grant, candidate, nil
		}
		if selected.selection.Root == nil || len(grant.authority.PhysicalRoot()) > len(selected.authority.PhysicalRoot()) {
			selected, relative = grant, candidate
		}
	}
	if selected.selection.Root == nil {
		return nativeDocumentRootGrant{}, rootedpath.RelativeDestination{}, fmt.Errorf("native document referent has no explicitly supplied trusted writer root")
	}
	return selected, relative, nil
}

func persistedNativeRoot(provenance rootedpath.AuthorityProvenance) recoveryRootProvenance {
	return recoveryRootProvenance{PhysicalRoot: provenance.PhysicalRoot(), ObjectFingerprint: provenance.ObjectFingerprint(), MountFingerprint: provenance.MountFingerprint()}
}

func persistedNativeObservation(observation mutation.DirectoryEntryAuthorityObservation) (recoveryDocumentObservation, error) {
	if err := observation.Validate(); err != nil {
		return recoveryDocumentObservation{}, err
	}
	if exact, known := observation.Exact(); known {
		return recoveryDocumentObservation{Kind: "exact", Authority: persistedPathAuthority(exact)}, nil
	}
	provisional, _ := observation.Provisional()
	return recoveryDocumentObservation{Kind: "provisional", Authority: &pathAuthorityDTO{Key: provisional.CandidateKey(), Witness: provisional.CandidateWitness()}, NamespaceAuthority: persistedPathAuthority(provisional.Namespace())}, nil
}
