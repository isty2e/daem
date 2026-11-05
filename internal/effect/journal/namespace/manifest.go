package namespace

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
)

// ManifestAuthority retains one selected project root and its capture/recovery
// provenance. Close releases only roots acquired by this owner.
type ManifestAuthority struct {
	root       *rootedpath.CapturedRoot
	authority  rootedpath.Authority
	provenance rootedpath.AuthorityProvenance
	owned      bool
	budget     *physicalwork.Budget
}

func CaptureManifestAuthority(path string, supplied *rootedpath.CapturedRoot, budget *physicalwork.Budget) (*ManifestAuthority, error) {
	if budget != nil {
		if supplied == nil {
			return nil, fmt.Errorf("native ordinary capture requires its retained manifest root")
		}
		if err := supplied.ValidateSelectionBounded(path, physicalwork.MaximumPhysicalPathDepth, budget); err != nil {
			return nil, err
		}
		authority, err := supplied.AuthorityBounded(budget)
		if err != nil {
			return nil, err
		}
		session, err := newManifestAuthority(supplied, authority, false)
		if err != nil {
			return nil, err
		}
		session.budget = budget
		return session, nil
	}

	root := supplied
	owned := false
	if root == nil {
		var err error
		root, err = rootedpath.CaptureRoot(path)
		if err != nil {
			return nil, fmt.Errorf("capture manifest root for recovery journal: %w", err)
		}
		owned = true
	} else if err := root.ValidateSelection(path); err != nil {
		return nil, fmt.Errorf("validate borrowed manifest root for recovery journal: %w", err)
	}
	authority, err := root.Authority()
	if err != nil {
		if owned {
			_ = root.Close()
		}
		return nil, fmt.Errorf("read captured manifest root authority: %w", err)
	}
	session, err := newManifestAuthority(root, authority, owned)
	if err != nil {
		if owned {
			_ = root.Close()
		}
		return nil, err
	}
	return session, nil
}

func RecaptureManifestAuthority(path string, expected rootedpath.AuthorityProvenance, budget *physicalwork.Budget) (*ManifestAuthority, error) {
	root, err := rootedpath.CaptureRootBounded(path, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return nil, fmt.Errorf("recapture manifest root for recovery: %w", err)
	}
	authority, err := root.AuthorityBounded(budget)
	if err != nil {
		_ = root.Close()
		return nil, fmt.Errorf("read recovery manifest root authority: %w", err)
	}
	session, err := newManifestAuthority(root, authority, true)
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	if err := expected.Match(session.authority); err != nil {
		closeErr := session.Close()
		return nil, errors.Join(fmt.Errorf("match recovery manifest root provenance: %w", err), closeErr)
	}
	session.budget = budget
	return session, nil
}

func newManifestAuthority(root *rootedpath.CapturedRoot, authority rootedpath.Authority, owned bool) (*ManifestAuthority, error) {
	provenance, err := authority.Provenance()
	if err != nil {
		return nil, fmt.Errorf("derive manifest root provenance: %w", err)
	}
	return &ManifestAuthority{root: root, authority: authority, provenance: provenance, owned: owned}, nil
}

func (session *ManifestAuthority) Provenance() rootedpath.AuthorityProvenance {
	if session == nil {
		return rootedpath.AuthorityProvenance{}
	}
	return session.provenance
}

func (session *ManifestAuthority) Acquire(destination output.Destination) (rootedpath.CommitCapability, error) {
	if session == nil || session.root == nil {
		return nil, fmt.Errorf("project root authority is unavailable for %q", destination)
	}
	if session.budget != nil {
		return session.AcquireBounded(destination, session.budget)
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

func (session *ManifestAuthority) AcquireBounded(destination output.Destination, budget *physicalwork.Budget) (rootedpath.CommitCapability, error) {
	if session == nil || session.root == nil {
		return nil, fmt.Errorf("project root authority is unavailable for %q", destination)
	}
	if session.budget != nil && budget != session.budget {
		return nil, fmt.Errorf("manifest root acquisition requires its original physical allocation")
	}
	relative, err := rootedpath.NewRelativeDestination(destination.RelativePath())
	if err != nil {
		return nil, err
	}
	bound, err := session.authority.Bind(relative)
	if err != nil {
		return nil, err
	}
	return session.root.AcquireBounded(bound, physicalwork.MaximumPhysicalPathDepth, budget)
}

func (session *ManifestAuthority) Close() error {
	if session == nil || !session.owned || session.root == nil {
		return nil
	}
	err := session.root.Close()
	session.root = nil
	return err
}
