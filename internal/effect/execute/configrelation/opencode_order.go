package configrelation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	observeopencode "github.com/isty2e/daem/internal/assurance/observe/opencodeplugin"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

// OpenCodeOrderPlan is one pair of exact observed server/TUI order candidates.
// It carries no planner policy, ownership claim, or plugin lifecycle authority.
type OpenCodeOrderPlan struct {
	observations []observeopencode.DocumentOrderObservation
	paths        []string
	scope        target.Scope
}

// NewOpenCodeOrderPlan admits one canonical OpenCode order observation for
// independent compare-and-swap execution in stable server-then-TUI order.
func NewOpenCodeOrderPlan(
	observation observeopencode.OrderObservation,
) (OpenCodeOrderPlan, error) {
	if err := observation.Validate(); err != nil {
		return OpenCodeOrderPlan{}, fmt.Errorf("OpenCode plugin order plan: %w", err)
	}
	documents := observation.Documents()
	if len(documents) != 2 ||
		documents[0].Kind() != opencodeconfig.ConfigServer ||
		documents[1].Kind() != opencodeconfig.ConfigTUI {
		return OpenCodeOrderPlan{}, fmt.Errorf(
			"OpenCode plugin order plan requires server then TUI observations",
		)
	}
	paths := make([]string, 0, len(documents))
	for index, document := range documents {
		path := document.Path()
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return OpenCodeOrderPlan{}, fmt.Errorf(
				"OpenCode plugin order document[%d] path is not canonical",
				index,
			)
		}
		if document.Scope() != observation.Scope() {
			return OpenCodeOrderPlan{}, fmt.Errorf(
				"OpenCode plugin order document[%d] scope does not match aggregate",
				index,
			)
		}
		paths = append(paths, path)
	}
	if filepath.Dir(paths[0]) != filepath.Dir(paths[1]) {
		return OpenCodeOrderPlan{}, fmt.Errorf(
			"OpenCode plugin order documents do not share one selected config root",
		)
	}
	return OpenCodeOrderPlan{
		observations: documents,
		paths:        paths,
		scope:        observation.Scope(),
	}, nil
}

// PhysicalAuthority returns both independently selected config paths.
func (plan OpenCodeOrderPlan) PhysicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	if err := plan.validate(); err != nil {
		return mutation.PhysicalAuthoritySet{}, err
	}
	requests := make([]mutation.PhysicalAuthorityRequest, 0, len(plan.paths))
	for _, path := range plan.paths {
		requests = append(requests, mutation.PhysicalAuthorityRequest{
			Path:   path,
			Target: string(target.TargetOpenCode),
			Scope:  string(plan.scope),
		})
	}
	return mutation.NewPhysicalAuthoritySet(requests...)
}

// Bind captures retained-root authority for both selected config documents.
func (plan OpenCodeOrderPlan) Bind(
	selectedRoot *rootedpath.CapturedRoot,
	selectedRootPath string,
) (*BoundOpenCodeOrder, error) {
	if err := plan.validate(); err != nil {
		return nil, err
	}
	authorities := make([]*rootedpath.EntryAuthority, 0, len(plan.paths))
	for index, path := range plan.paths {
		authority, err := rootedpath.BindSelectedEntryAuthority(
			selectedRoot,
			selectedRootPath,
			path,
		)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("bind OpenCode plugin order document[%d]: %w", index, err),
				closeAuthorities(authorities),
			)
		}
		authorities = append(authorities, authority)
	}
	return &BoundOpenCodeOrder{plan: plan, authorities: authorities}, nil
}

func (plan OpenCodeOrderPlan) validate() error {
	if len(plan.observations) != 2 ||
		len(plan.paths) != len(plan.observations) ||
		plan.scope == "" {
		return fmt.Errorf("OpenCode plugin order plan is incomplete")
	}
	for index, observation := range plan.observations {
		if err := observation.Validate(); err != nil {
			return fmt.Errorf("OpenCode plugin order plan document[%d]: %w", index, err)
		}
		if observation.Path() != plan.paths[index] ||
			observation.Scope() != plan.scope {
			return fmt.Errorf(
				"OpenCode plugin order plan document[%d] does not match its observation",
				index,
			)
		}
	}
	return nil
}

// BoundOpenCodeOrder owns independent entry bindings for one ordered mutation
// attempt. Earlier document convergence remains visible if a later step fails.
type BoundOpenCodeOrder struct {
	plan        OpenCodeOrderPlan
	authorities []*rootedpath.EntryAuthority
	closed      bool
}

// Execute converges server then TUI and stops on the first failure. The changed
// count reports writes known to have become visible; a replacement error may
// leave visibility unknown and is never upgraded to verified convergence.
func (order *BoundOpenCodeOrder) Execute(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
) (changedDocuments int, resultErr error) {
	if order == nil || order.closed {
		return 0, fmt.Errorf("bound OpenCode plugin order is unavailable")
	}
	if ctx == nil {
		return 0, fmt.Errorf("OpenCode plugin order context is required")
	}
	if filesystem == nil {
		return 0, fmt.Errorf("OpenCode plugin order filesystem is required")
	}
	if err := order.plan.validate(); err != nil {
		return 0, err
	}
	if len(order.authorities) != len(order.plan.observations) {
		return 0, fmt.Errorf("bound OpenCode plugin order authority is incomplete")
	}

	for index, observation := range order.plan.observations {
		changed, err := executeOpenCodeDocumentOrder(
			ctx,
			filesystem,
			order.authorities[index],
			observation,
		)
		if changed {
			changedDocuments++
		}
		if err != nil {
			return changedDocuments, fmt.Errorf(
				"converge OpenCode plugin order document[%d] %s: %w",
				index,
				observation.Kind(),
				err,
			)
		}
	}
	return changedDocuments, nil
}

// Close releases both independently captured entry roots.
func (order *BoundOpenCodeOrder) Close() error {
	if order == nil || order.closed {
		return nil
	}
	order.closed = true
	err := closeAuthorities(order.authorities)
	order.authorities = nil
	return err
}

func executeOpenCodeDocumentOrder(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	observation observeopencode.DocumentOrderObservation,
) (bool, error) {
	capability, err := authority.Acquire()
	if err != nil {
		return false, err
	}
	content, mode, identity, exists, err := readOpenCodeConfig(
		ctx,
		filesystem,
		capability,
	)
	if err != nil {
		return false, errors.Join(err, capability.Close())
	}
	if err := observation.VerifyBaseline(content, exists); err != nil {
		if postErr := observation.VerifyPostContent(content, exists); postErr == nil {
			return false, capability.Close()
		}
		return false, errors.Join(err, capability.Close())
	}
	if !observation.Changed() {
		if err := observation.VerifyPostContent(content, exists); err != nil {
			return false, errors.Join(err, capability.Close())
		}
		return false, capability.Close()
	}

	candidate, candidateExists := observation.Candidate()
	if !candidateExists {
		return false, errors.Join(
			fmt.Errorf("OpenCode plugin order cannot create missing %s config", observation.Kind()),
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

	postCapability, err := authority.Acquire()
	if err != nil {
		return true, fmt.Errorf(
			"acquire OpenCode %s config post-observation: %w",
			observation.Kind(),
			err,
		)
	}
	postContent, _, _, postExists, readErr := readOpenCodeConfig(
		ctx,
		filesystem,
		postCapability,
	)
	closeErr := postCapability.Close()
	if readErr != nil {
		return true, errors.Join(
			fmt.Errorf(
				"read OpenCode %s config post-observation: %w",
				observation.Kind(),
				readErr,
			),
			closeErr,
		)
	}
	if closeErr != nil {
		return true, closeErr
	}
	if err := observation.VerifyPostContent(postContent, postExists); err != nil {
		return true, err
	}
	return true, nil
}

func readOpenCodeConfig(
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
		observeopencode.MaximumConfigBytes,
	)
	if errors.Is(err, os.ErrNotExist) {
		return nil, 0, nil, false, nil
	}
	if err != nil {
		return nil, 0, nil, false, err
	}
	return content, mode, identity, true, nil
}
