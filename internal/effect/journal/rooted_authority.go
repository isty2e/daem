package journal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

// projectAuthoritySession retains project-root provenance and native authority
// for one capture or final recovery classification.
type projectAuthoritySession struct {
	root       *rootedpath.CapturedRoot
	authority  rootedpath.Authority
	provenance rootedpath.AuthorityProvenance
	owned      bool
}

// recoveryGlobalPathBindings freezes resolver output before journal evidence is
// captured. The persisted lexical path binds root selection only; exact claim
// identity remains owned by OwnershipPathAuthority.
type recoveryGlobalPathBindings map[output.Destination]string

func captureRecoveryGlobalPathBindings(
	actions []pathMutation,
	resolver func(output.Destination) (string, error),
) (recoveryGlobalPathBindings, error) {
	bindings := make(recoveryGlobalPathBindings)
	for _, action := range actions {
		if action.Scope != target.ScopeGlobal {
			continue
		}
		if _, present := bindings[action.Destination]; present {
			continue
		}
		resolved, err := resolveRecoveryGlobalPath(action.Destination, resolver)
		if err != nil {
			return nil, err
		}
		bindings[action.Destination] = resolved
	}
	return bindings, nil
}

func (bindings recoveryGlobalPathBindings) resolver(
	fallback func(output.Destination) (string, error),
) func(output.Destination) (string, error) {
	return func(destination output.Destination) (string, error) {
		if resolved, present := bindings[destination]; present {
			return resolved, nil
		}
		return fallback(destination)
	}
}

func (bindings recoveryGlobalPathBindings) path(
	scope target.Scope,
	destination output.Destination,
) (string, error) {
	if scope != target.ScopeGlobal {
		return "", nil
	}
	resolved, present := bindings[destination]
	if !present {
		return "", fmt.Errorf("global destination %q has no capture-time path binding", destination)
	}
	return resolved, nil
}

func validateRecoveryGlobalPathBindings(
	ctx context.Context,
	entries []recoveryEntry,
	resolver func(output.Destination) (string, error),
) error {
	observed := make(map[output.Destination]string)
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
		current, present := observed[destination]
		if !present {
			current, err = resolveRecoveryGlobalPath(destination, resolver)
			if err != nil {
				return fmt.Errorf("recovery entries[%d]: %w", index, err)
			}
			observed[destination] = current
		}
		if current != entry.ResolvedGlobalPath {
			return fmt.Errorf(
				"recovery entries[%d] global destination %q resolved to %q, not capture-time path %q; root selection changed",
				index,
				destination,
				current,
				entry.ResolvedGlobalPath,
			)
		}
	}
	return nil
}

func resolveRecoveryGlobalPath(
	destination output.Destination,
	resolver func(output.Destination) (string, error),
) (string, error) {
	if resolver == nil {
		return "", fmt.Errorf("recovery global destination resolver is required")
	}
	resolved, err := resolver(destination)
	if err != nil {
		return "", fmt.Errorf("resolve global destination %q: %w", destination, err)
	}
	root, bound, err := rootedpath.CaptureDestination(resolved)
	if err != nil {
		return "", fmt.Errorf("bind global destination %q: %w", destination, err)
	}
	path, pathErr := bound.LexicalPath()
	if pathErr != nil {
		pathErr = fmt.Errorf("read global destination %q lexical path: %w", destination, pathErr)
	}
	closeErr := root.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close global destination %q root authority: %w", destination, closeErr)
	}
	if pathErr != nil || closeErr != nil {
		return "", errors.Join(pathErr, closeErr)
	}
	return path, nil
}

func projectAuthorityForCapture(
	paths Paths,
	actions []pathMutation,
	supplied *rootedpath.CapturedRoot,
) (*projectAuthoritySession, error) {
	required := false
	for _, action := range actions {
		if action.Scope == target.ScopeProject {
			required = true
			break
		}
	}
	if !required {
		if supplied != nil {
			return nil, fmt.Errorf("project root witness supplied for a journal without project entries")
		}
		return nil, nil
	}

	root := supplied
	owned := false
	if root == nil {
		var err error
		root, err = rootedpath.CaptureRoot(paths.ManifestRoot)
		if err != nil {
			return nil, fmt.Errorf("capture project root for recovery journal: %w", err)
		}
		owned = true
	}
	session, err := newProjectAuthoritySession(root, owned)
	if err != nil {
		if owned {
			_ = root.Close()
		}
		return nil, err
	}
	return session, nil
}

func projectAuthorityForRecovery(
	paths Paths,
	journal recoveryJournal,
) (*projectAuthoritySession, error) {
	if journal.ProjectRootProvenance == nil {
		return nil, nil
	}
	expected, err := journal.ProjectRootProvenance.canonical()
	if err != nil {
		return nil, err
	}
	root, err := rootedpath.CaptureRoot(paths.ManifestRoot)
	if err != nil {
		return nil, fmt.Errorf("recapture project root for recovery: %w", err)
	}
	session, err := newProjectAuthoritySession(root, true)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if err := expected.Match(session.authority); err != nil {
		closeErr := session.close()
		return nil, errors.Join(
			fmt.Errorf("match recovery project root provenance: %w", err),
			closeErr,
		)
	}
	return session, nil
}

func newProjectAuthoritySession(root *rootedpath.CapturedRoot, owned bool) (*projectAuthoritySession, error) {
	if root == nil {
		return nil, fmt.Errorf("captured project root is required")
	}
	authority, err := root.Authority()
	if err != nil {
		return nil, fmt.Errorf("read captured project root authority: %w", err)
	}
	provenance, err := authority.Provenance()
	if err != nil {
		return nil, fmt.Errorf("derive project root provenance: %w", err)
	}
	return &projectAuthoritySession{
		root:       root,
		authority:  authority,
		provenance: provenance,
		owned:      owned,
	}, nil
}

func (session *projectAuthoritySession) persisted() *recoveryProjectRootProvenance {
	if session == nil {
		return nil
	}
	return &recoveryProjectRootProvenance{
		PhysicalRoot:      session.provenance.PhysicalRoot(),
		ObjectFingerprint: session.provenance.ObjectFingerprint(),
		MountFingerprint:  session.provenance.MountFingerprint(),
	}
}

func (session *projectAuthoritySession) acquire(
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

func (session *projectAuthoritySession) close() error {
	if session == nil || !session.owned || session.root == nil {
		return nil
	}
	err := session.root.Close()
	session.root = nil
	return err
}

func (provenance recoveryProjectRootProvenance) canonical() (rootedpath.AuthorityProvenance, error) {
	return rootedpath.NewAuthorityProvenance(
		provenance.PhysicalRoot,
		provenance.ObjectFingerprint,
		provenance.MountFingerprint,
	)
}

func validateProjectRootProvenanceCoverage(journal recoveryJournal) error {
	hasProjectEntries := false
	for index, entry := range journal.Entries {
		scope, err := target.ParseScope(entry.Scope)
		if err != nil {
			return fmt.Errorf("recovery entries[%d].scope: %w", index, err)
		}
		if scope != target.ScopeProject {
			continue
		}
		hasProjectEntries = true
		if _, err := rootedpath.NewRelativeDestination(entry.Path); err != nil {
			return fmt.Errorf("recovery entries[%d] project destination: %w", index, err)
		}
	}
	if hasProjectEntries != (journal.ProjectRootProvenance != nil) {
		if hasProjectEntries {
			return fmt.Errorf("recovery journal project entries require project_root_provenance")
		}
		return fmt.Errorf("recovery journal without project entries must not contain project_root_provenance")
	}
	if journal.ProjectRootProvenance != nil {
		if _, err := journal.ProjectRootProvenance.canonical(); err != nil {
			return fmt.Errorf("recovery journal project_root_provenance: %w", err)
		}
	}
	return nil
}

// RootedCapabilityResolver acquires a borrowed, current capability for one
// already-bound global destination. The caller retains the root authority; the
// receiver closes each returned capability.
type RootedCapabilityResolver func(
	destination output.Destination,
) (rootedpath.CommitCapability, bool, error)

func acquireMatchingRootedCapability(
	destination output.Destination,
	resolvedPath string,
	acquire RootedCapabilityResolver,
) (rootedpath.CommitCapability, bool, error) {
	if acquire == nil {
		return nil, false, nil
	}
	capability, present, err := acquire(destination)
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
