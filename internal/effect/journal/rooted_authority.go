package journal

import (
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
