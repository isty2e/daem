package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// manifestAuthoritySession retains manifest-root provenance and native authority
// for one capture or final recovery classification.
type manifestAuthoritySession struct {
	root       *rootedpath.CapturedRoot
	authority  rootedpath.Authority
	provenance rootedpath.AuthorityProvenance
	owned      bool
}

// recoveryGlobalPathObservation binds one logical global destination to the
// exact retained root incarnation used for capture or recovery observation.
// It is runtime authority, not persistence or ownership identity.
type recoveryGlobalPathObservation struct {
	resolvedPath  string
	rootAuthority rootedpath.Authority
}

type recoveryGlobalPathBindings map[output.Destination]recoveryGlobalPathObservation

func captureRecoveryGlobalPathBindings(
	actions []pathMutation,
	resolver func(output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
) (recoveryGlobalPathBindings, error) {
	bindings := make(recoveryGlobalPathBindings)
	for _, action := range actions {
		if action.Scope != target.ScopeGlobal {
			continue
		}
		if _, present := bindings[action.Destination]; present {
			continue
		}
		observed, err := observeRecoveryGlobalPathBinding(
			action.Destination,
			resolver,
			rootedCapability,
		)
		if err != nil {
			return nil, err
		}
		bindings[action.Destination] = observed
	}
	return bindings, nil
}

func (bindings recoveryGlobalPathBindings) resolver(
	fallback func(output.Destination) (string, error),
) func(output.Destination) (string, error) {
	return func(destination output.Destination) (string, error) {
		if binding, present := bindings[destination]; present {
			return binding.resolvedPath, nil
		}
		return fallback(destination)
	}
}

func (bindings recoveryGlobalPathBindings) persisted(
	scope target.Scope,
	destination output.Destination,
) (*recoveryGlobalPathBinding, error) {
	if scope != target.ScopeGlobal {
		return nil, nil
	}
	binding, present := bindings[destination]
	if !present {
		return nil, fmt.Errorf("global destination %q has no capture-time path binding", destination)
	}
	return binding.persisted()
}

func validateRecoveryGlobalPathBindings(
	ctx context.Context,
	entries []recoveryEntry,
	resolver func(output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
	budget *recovery.PhysicalWorkBudget,
) error {
	validated := make(map[output.Destination]recoveryGlobalPathBinding)
	currentRoots := make(map[string]rootedpath.Authority)
	for index, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Scope != string(target.ScopeGlobal) {
			continue
		}
		destination, err := output.Parse(entry.Path)
		if err != nil {
			return fmt.Errorf("recovery entries[%d] global destination: %w", index, err)
		}
		if entry.GlobalPathBinding == nil {
			return fmt.Errorf("recovery entries[%d] global destination has no capture-time path binding", index)
		}
		if previous, present := validated[destination]; present {
			if previous != *entry.GlobalPathBinding {
				return fmt.Errorf(
					"recovery entries[%d] global destination %q has inconsistent capture-time bindings",
					index,
					destination,
				)
			}
			continue
		}
		current, err := observeRecoveryGlobalPathBindingForRecovery(
			destination,
			resolver,
			rootedCapability,
			budget,
		)
		if err != nil {
			return fmt.Errorf("recovery entries[%d]: %w", index, err)
		}
		if err := entry.GlobalPathBinding.match(current, currentRoots, budget); err != nil {
			return fmt.Errorf("recovery entries[%d] global destination %q: %w", index, destination, err)
		}
		validated[destination] = *entry.GlobalPathBinding
	}
	return nil
}

func observeRecoveryGlobalPathBindingForRecovery(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
	budget *recovery.PhysicalWorkBudget,
) (recoveryGlobalPathObservation, error) {
	if budget == nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("recovery path work budget is required")
	}
	if resolver == nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("recovery global destination resolver is required")
	}
	resolved, err := resolver(destination)
	if err != nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("resolve global destination %q: %w", destination, err)
	}
	if rootedCapability != nil {
		capability, present, err := acquireMatchingRootedCapability(
			destination,
			resolved,
			rootedCapability,
			budget,
		)
		if err != nil {
			return recoveryGlobalPathObservation{}, err
		}
		if !present {
			return recoveryGlobalPathObservation{}, fmt.Errorf(
				"global destination %q has no retained root authority",
				destination,
			)
		}
		bound := capability.Destination()
		path, pathErr := bound.LexicalPath()
		closeErr := capability.Close()
		if pathErr != nil || closeErr != nil {
			return recoveryGlobalPathObservation{}, errors.Join(pathErr, closeErr)
		}
		return recoveryGlobalPathObservation{
			resolvedPath:  path,
			rootAuthority: bound.Root(),
		}, nil
	}
	root, bound, err := rootedpath.CaptureDestinationBounded(
		resolved,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("bind global destination %q: %w", destination, err)
	}
	path, pathErr := bound.LexicalPath()
	if pathErr != nil {
		pathErr = fmt.Errorf("read global destination %q lexical path: %w", destination, pathErr)
	}
	authority := bound.Root()
	closeErr := root.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close global destination %q root authority: %w", destination, closeErr)
	}
	if pathErr != nil || closeErr != nil {
		return recoveryGlobalPathObservation{}, errors.Join(pathErr, closeErr)
	}
	return recoveryGlobalPathObservation{resolvedPath: path, rootAuthority: authority}, nil
}

func observeRecoveryGlobalPathBinding(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
	rootedCapability RootedCapabilityResolver,
) (recoveryGlobalPathObservation, error) {
	if resolver == nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("recovery global destination resolver is required")
	}
	resolved, err := resolver(destination)
	if err != nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("resolve global destination %q: %w", destination, err)
	}
	if rootedCapability != nil {
		capability, present, err := acquireMatchingRootedCapability(
			destination,
			resolved,
			rootedCapability,
			nil,
		)
		if err != nil {
			return recoveryGlobalPathObservation{}, err
		}
		if !present {
			return recoveryGlobalPathObservation{}, fmt.Errorf(
				"global destination %q has no retained root authority",
				destination,
			)
		}
		if err := capability.Validate(); err != nil {
			return recoveryGlobalPathObservation{}, errors.Join(err, capability.Close())
		}
		bound := capability.Destination()
		path, pathErr := bound.LexicalPath()
		closeErr := capability.Close()
		if pathErr != nil || closeErr != nil {
			return recoveryGlobalPathObservation{}, errors.Join(pathErr, closeErr)
		}
		return recoveryGlobalPathObservation{
			resolvedPath:  path,
			rootAuthority: bound.Root(),
		}, nil
	}
	root, bound, err := rootedpath.CaptureDestination(resolved)
	if err != nil {
		return recoveryGlobalPathObservation{}, fmt.Errorf("bind global destination %q: %w", destination, err)
	}
	path, pathErr := bound.LexicalPath()
	if pathErr != nil {
		pathErr = fmt.Errorf("read global destination %q lexical path: %w", destination, pathErr)
	}
	authority, authorityErr := root.Authority()
	if authorityErr != nil {
		authorityErr = fmt.Errorf("read global destination %q root authority: %w", destination, authorityErr)
	}
	closeErr := root.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close global destination %q root authority: %w", destination, closeErr)
	}
	if pathErr != nil || authorityErr != nil || closeErr != nil {
		return recoveryGlobalPathObservation{}, errors.Join(pathErr, authorityErr, closeErr)
	}
	return recoveryGlobalPathObservation{resolvedPath: path, rootAuthority: authority}, nil
}

func (binding recoveryGlobalPathObservation) persisted() (*recoveryGlobalPathBinding, error) {
	provenance, err := binding.rootAuthority.Provenance()
	if err != nil {
		return nil, err
	}
	return &recoveryGlobalPathBinding{
		ResolvedPath:   binding.resolvedPath,
		RootProvenance: persistedRecoveryRootProvenance(provenance),
	}, nil
}

func (binding recoveryGlobalPathBinding) canonical() (string, rootedpath.AuthorityProvenance, error) {
	if err := validateRecoveryResolvedGlobalPath(binding.ResolvedPath); err != nil {
		return "", rootedpath.AuthorityProvenance{}, err
	}
	provenance, err := binding.RootProvenance.canonical()
	if err != nil {
		return "", rootedpath.AuthorityProvenance{}, err
	}
	relative, err := filepath.Rel(provenance.PhysicalRoot(), binding.ResolvedPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", rootedpath.AuthorityProvenance{}, fmt.Errorf(
			"resolved path %q is not below captured root %q",
			binding.ResolvedPath,
			provenance.PhysicalRoot(),
		)
	}
	return binding.ResolvedPath, provenance, nil
}

func (binding recoveryGlobalPathBinding) match(
	current recoveryGlobalPathObservation,
	currentRoots map[string]rootedpath.Authority,
	budget *recovery.PhysicalWorkBudget,
) error {
	expectedPath, expectedRoot, err := binding.canonical()
	if err != nil {
		return err
	}
	if current.resolvedPath != expectedPath {
		return fmt.Errorf(
			"resolved to %q, not capture-time path %q; root selection changed",
			current.resolvedPath,
			expectedPath,
		)
	}
	if err := expectedRoot.MatchDescendant(current.rootAuthority); err != nil {
		return fmt.Errorf("current destination root violates capture-time authority: %w", err)
	}
	currentRoot, present := currentRoots[expectedRoot.PhysicalRoot()]
	if !present && current.rootAuthority.PhysicalRoot() == expectedRoot.PhysicalRoot() {
		currentRoot = current.rootAuthority
		currentRoots[expectedRoot.PhysicalRoot()] = currentRoot
		present = true
	}
	if !present {
		recaptured, err := rootedpath.CaptureCanonicalRootNoFollowBounded(
			expectedRoot.PhysicalRoot(),
			recovery.MaximumPhysicalPathDepth,
			budget,
		)
		if err != nil {
			return fmt.Errorf("recapture capture-time global root: %w", err)
		}
		currentRoot, err = recaptured.AuthorityBounded(budget)
		if err != nil {
			return errors.Join(err, recaptured.Close())
		}
		currentRoots[expectedRoot.PhysicalRoot()] = currentRoot
		if err := recaptured.Close(); err != nil {
			return fmt.Errorf("close recaptured global root authority: %w", err)
		}
	}
	if err := expectedRoot.Match(currentRoot); err != nil {
		return fmt.Errorf("capture-time root provenance changed: %w", err)
	}
	return nil
}

func manifestAuthorityForCapture(
	paths Paths,
	supplied *rootedpath.CapturedRoot,
) (*manifestAuthoritySession, error) {
	root := supplied
	owned := false
	if root == nil {
		var err error
		root, err = rootedpath.CaptureRoot(paths.ManifestRoot)
		if err != nil {
			return nil, fmt.Errorf("capture manifest root for recovery journal: %w", err)
		}
		owned = true
	} else if err := root.ValidateSelection(paths.ManifestRoot); err != nil {
		return nil, fmt.Errorf("validate borrowed manifest root for recovery journal: %w", err)
	}
	session, err := newManifestAuthoritySession(root, owned)
	if err != nil {
		if owned {
			_ = root.Close()
		}
		return nil, err
	}
	return session, nil
}

func manifestAuthorityForRecovery(
	paths Paths,
	journal recoveryJournal,
	budget *recovery.PhysicalWorkBudget,
) (*manifestAuthoritySession, error) {
	expected, err := journal.ManifestRootProvenance.canonical()
	if err != nil {
		return nil, err
	}
	root, err := rootedpath.CaptureRootBounded(
		paths.ManifestRoot,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
	if err != nil {
		return nil, fmt.Errorf("recapture manifest root for recovery: %w", err)
	}
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("read recovery manifest root authority: %w", err)
	}
	session, err := newManifestAuthoritySessionFromAuthority(root, authority, true)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if err := expected.Match(session.authority); err != nil {
		closeErr := session.close()
		return nil, errors.Join(
			fmt.Errorf("match recovery manifest root provenance: %w", err),
			closeErr,
		)
	}
	return session, nil
}

func newManifestAuthoritySession(root *rootedpath.CapturedRoot, owned bool) (*manifestAuthoritySession, error) {
	if root == nil {
		return nil, fmt.Errorf("captured manifest root is required")
	}
	authority, err := root.Authority()
	if err != nil {
		return nil, fmt.Errorf("read captured manifest root authority: %w", err)
	}
	return newManifestAuthoritySessionFromAuthority(root, authority, owned)
}

func newManifestAuthoritySessionFromAuthority(
	root *rootedpath.CapturedRoot,
	authority rootedpath.Authority,
	owned bool,
) (*manifestAuthoritySession, error) {
	provenance, err := authority.Provenance()
	if err != nil {
		return nil, fmt.Errorf("derive manifest root provenance: %w", err)
	}
	return &manifestAuthoritySession{
		root:       root,
		authority:  authority,
		provenance: provenance,
		owned:      owned,
	}, nil
}

func (session *manifestAuthoritySession) persisted() recoveryRootProvenance {
	return persistedRecoveryRootProvenance(session.provenance)
}

func persistedRecoveryRootProvenance(provenance rootedpath.AuthorityProvenance) recoveryRootProvenance {
	return recoveryRootProvenance{
		PhysicalRoot:      provenance.PhysicalRoot(),
		ObjectFingerprint: provenance.ObjectFingerprint(),
		MountFingerprint:  provenance.MountFingerprint(),
	}
}

func (session *manifestAuthoritySession) acquire(
	destination output.Destination,
) (rootedpath.CommitCapability, error) {
	if session == nil || session.root == nil {
		return nil, fmt.Errorf("project root authority is unavailable for %q", destination)
	}
	relative, err := rootedpath.NewRelativeDestination(destination.RelativePath())
	if err != nil {
		return nil, err
	}
	bound, err := session.authority.Bind(relative)
	if err != nil {
		return nil, err
	}
	return session.root.Acquire(bound)
}

func (session *manifestAuthoritySession) acquireBounded(
	destination output.Destination,
	budget *recovery.PhysicalWorkBudget,
) (rootedpath.CommitCapability, error) {
	if session == nil || session.root == nil {
		return nil, fmt.Errorf("project root authority is unavailable for %q", destination)
	}
	relative, err := rootedpath.NewRelativeDestination(destination.RelativePath())
	if err != nil {
		return nil, err
	}
	bound, err := session.authority.Bind(relative)
	if err != nil {
		return nil, err
	}
	return session.root.AcquireBounded(
		bound,
		recovery.MaximumPhysicalPathDepth,
		budget,
	)
}

func (session *manifestAuthoritySession) close() error {
	if session == nil || !session.owned || session.root == nil {
		return nil
	}
	err := session.root.Close()
	session.root = nil
	return err
}

func (provenance recoveryRootProvenance) canonical() (rootedpath.AuthorityProvenance, error) {
	return rootedpath.NewAuthorityProvenance(
		provenance.PhysicalRoot,
		provenance.ObjectFingerprint,
		provenance.MountFingerprint,
	)
}

func validateManifestRootProvenance(journal recoveryJournal) error {
	for index, entry := range journal.Entries {
		scope, err := target.ParseScope(entry.Scope)
		if err != nil {
			return fmt.Errorf("recovery entries[%d].scope: %w", index, err)
		}
		if scope != target.ScopeProject {
			continue
		}
		if _, err := rootedpath.NewRelativeDestination(entry.Path); err != nil {
			return fmt.Errorf("recovery entries[%d] project destination: %w", index, err)
		}
	}
	if _, err := journal.ManifestRootProvenance.canonical(); err != nil {
		return fmt.Errorf("recovery journal manifest_root_provenance: %w", err)
	}
	return nil
}

// RootedCapabilityResolver acquires a borrowed, current capability for one
// already-bound global destination. A non-nil budget must cover acquisition
// before descriptor validation begins. The caller retains the root authority;
// the receiver closes each returned capability.
type RootedCapabilityResolver func(
	destination output.Destination,
	budget rootedpath.PhysicalTraversalBudget,
) (rootedpath.CommitCapability, bool, error)

func acquireMatchingRootedCapability(
	destination output.Destination,
	resolvedPath string,
	acquire RootedCapabilityResolver,
	budget rootedpath.PhysicalTraversalBudget,
) (rootedpath.CommitCapability, bool, error) {
	if acquire == nil {
		return nil, false, nil
	}
	capability, present, err := acquire(destination, budget)
	if err != nil {
		if capability != nil {
			err = errors.Join(err, capability.Close())
		}
		return nil, false, err
	}
	if !present {
		if capability == nil {
			return nil, false, nil
		}
		return nil, false, errors.Join(
			fmt.Errorf("destination %q returned retained root authority while reporting it absent", destination),
			capability.Close(),
		)
	}
	if capability == nil {
		return nil, false, fmt.Errorf("destination %q returned nil retained root authority", destination)
	}
	capabilityPath, err := capability.Destination().LexicalPath()
	if err != nil {
		return nil, false, errors.Join(
			fmt.Errorf("read destination %q retained authority path: %w", destination, err),
			capability.Close(),
		)
	}
	if filepath.Clean(capabilityPath) != filepath.Clean(resolvedPath) {
		return nil, false, errors.Join(
			fmt.Errorf(
				"destination %q resolver path %q does not match retained authority path %q",
				destination,
				resolvedPath,
				capabilityPath,
			),
			capability.Close(),
		)
	}
	return capability, true, nil
}
