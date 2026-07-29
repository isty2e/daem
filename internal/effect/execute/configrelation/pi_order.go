package configrelation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	observepipackage "github.com/isty2e/daem/internal/assurance/observe/pipackage"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/target"
)

// PiOrderPlan is one exact observed package-order candidate. It carries no
// planner policy, ownership claim, or package lifecycle authority.
type PiOrderPlan struct {
	observation observepipackage.OrderObservation
	path        string
	scope       target.Scope
}

// NewPiOrderPlan admits one canonical Pi order observation for direct
// compare-and-swap execution.
func NewPiOrderPlan(
	observation observepipackage.OrderObservation,
) (PiOrderPlan, error) {
	if err := observation.Validate(); err != nil {
		return PiOrderPlan{}, fmt.Errorf("Pi package order plan: %w", err)
	}
	path := observation.SettingsPath()
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return PiOrderPlan{}, fmt.Errorf("Pi package order settings path is not canonical")
	}
	switch observation.Scope() {
	case target.ScopeProject, target.ScopeGlobal:
	default:
		return PiOrderPlan{}, fmt.Errorf(
			"Pi package order scope %q is not mutable",
			observation.Scope(),
		)
	}
	return PiOrderPlan{
		observation: observation,
		path:        path,
		scope:       observation.Scope(),
	}, nil
}

// PhysicalAuthority returns the single observed settings path required by the
// plan.
func (plan PiOrderPlan) PhysicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	if err := plan.validate(); err != nil {
		return mutation.PhysicalAuthoritySet{}, err
	}
	return mutation.NewPhysicalAuthoritySet(mutation.PhysicalAuthorityRequest{
		Path:   plan.path,
		Target: string(target.TargetPi),
		Scope:  string(plan.scope),
	})
}

// Bind captures retained-root authority for the observed settings path.
func (plan PiOrderPlan) Bind(
	selectedRoot *rootedpath.CapturedRoot,
	selectedRootPath string,
) (*BoundPiOrder, error) {
	if err := plan.validate(); err != nil {
		return nil, err
	}
	authority, err := rootedpath.BindSelectedEntryAuthority(
		selectedRoot,
		selectedRootPath,
		plan.path,
	)
	if err != nil {
		return nil, fmt.Errorf("bind Pi package order settings: %w", err)
	}
	return &BoundPiOrder{plan: plan, authority: authority}, nil
}

func (plan PiOrderPlan) validate() error {
	if plan.path == "" || plan.scope == "" {
		return fmt.Errorf("Pi package order plan is incomplete")
	}
	if err := plan.observation.Validate(); err != nil {
		return fmt.Errorf("Pi package order plan observation: %w", err)
	}
	if plan.path != plan.observation.SettingsPath() ||
		plan.scope != plan.observation.Scope() {
		return fmt.Errorf("Pi package order plan does not match its observation")
	}
	return nil
}

// BoundPiOrder owns one settings entry binding for repeated exact execution.
type BoundPiOrder struct {
	plan      PiOrderPlan
	authority *rootedpath.EntryAuthority
	closed    bool
}

// Execute publishes the exact candidate under baseline CAS and then requires
// an exact fresh post-observation. A prior attempt that already published the
// candidate is recognized as converged.
func (order *BoundPiOrder) Execute(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
) (bool, error) {
	if order == nil || order.closed || order.authority == nil {
		return false, fmt.Errorf("bound Pi package order is unavailable")
	}
	if ctx == nil {
		return false, fmt.Errorf("Pi package order context is required")
	}
	if filesystem == nil {
		return false, fmt.Errorf("Pi package order filesystem is required")
	}
	if err := order.plan.validate(); err != nil {
		return false, err
	}

	capability, err := order.authority.Acquire()
	if err != nil {
		return false, err
	}
	content, mode, identity, exists, err := readPiSettings(
		ctx,
		filesystem,
		capability,
	)
	if err != nil {
		return false, errors.Join(err, capability.Close())
	}
	if err := order.plan.observation.VerifyBaseline(content, exists); err != nil {
		if postErr := order.plan.observation.VerifyPostContent(content, exists); postErr == nil {
			return false, capability.Close()
		}
		return false, errors.Join(err, capability.Close())
	}
	if !order.plan.observation.Changed() {
		if err := order.plan.observation.VerifyPostContent(content, exists); err != nil {
			return false, errors.Join(err, capability.Close())
		}
		return false, capability.Close()
	}

	candidate, candidateExists := order.plan.observation.Candidate()
	if !candidateExists {
		return false, errors.Join(
			fmt.Errorf("Pi package order cannot create missing settings"),
			capability.Close(),
		)
	}
	if err := filesystem.ReplaceRootedFile(
		ctx,
		capability,
		candidate,
		mode,
		identity,
	); err != nil {
		return false, err
	}

	postCapability, err := order.authority.Acquire()
	if err != nil {
		return true, fmt.Errorf("acquire Pi package order post-observation: %w", err)
	}
	postContent, _, _, postExists, readErr := readPiSettings(
		ctx,
		filesystem,
		postCapability,
	)
	closeErr := postCapability.Close()
	if readErr != nil {
		return true, errors.Join(
			fmt.Errorf("read Pi package order post-observation: %w", readErr),
			closeErr,
		)
	}
	if closeErr != nil {
		return true, closeErr
	}
	if err := order.plan.observation.VerifyPostContent(postContent, postExists); err != nil {
		return true, err
	}
	return true, nil
}

// Close releases the retained entry root.
func (order *BoundPiOrder) Close() error {
	if order == nil || order.closed {
		return nil
	}
	order.closed = true
	err := order.authority.Close()
	order.authority = nil
	return err
}

func readPiSettings(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	capability rootedpath.CommitCapability,
) (
	content []byte,
	mode os.FileMode,
	identity mutationfs.EntryIdentity,
	exists bool,
	err error,
) {
	content, mode, identity, err = filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		observepipackage.MaximumSettingsBytes,
	)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil, false, nil
	}
	if err != nil {
		return nil, 0, nil, false, err
	}
	return content, mode, identity, true, nil
}
