// Package configrelation executes admitted direct host-config relation removal.
package configrelation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	opencodeconfig "github.com/isty2e/daem/internal/realization/configrelation/opencode"
	"github.com/isty2e/daem/internal/target"
)

const maximumConfigBytes = 16 << 20

type removalKind uint8

const (
	removalKindOpenCodePlugin removalKind = iota + 1
)

// RemovalInput contains current observer authority and exact canonical relation
// identity. It carries no ownership or reconciliation decision.
type RemovalInput struct {
	Target         target.Target
	Scope          target.Scope
	Carrier        desiredextension.Carrier
	Source         string
	AuthorityPaths []observerelation.AuthorityPath
}

// RemovalPlan is one validated direct relation mutation shape. Paths are
// selected from current observer authority, not recomputed from environment.
type RemovalPlan struct {
	kind   removalKind
	target target.Target
	scope  target.Scope
	source string
	paths  []string
}

// NewRemovalPlan selects the exact admitted host-config documents.
func NewRemovalPlan(input RemovalInput) (RemovalPlan, error) {
	if _, err := target.ParseTarget(string(input.Target)); err != nil {
		return RemovalPlan{}, err
	}
	if _, err := target.ParseScope(string(input.Scope)); err != nil {
		return RemovalPlan{}, err
	}
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindHostSource,
		input.Source,
	)
	if err != nil {
		return RemovalPlan{}, fmt.Errorf("direct config relation source: %w", err)
	}
	if !source.CredentialFree() {
		return RemovalPlan{}, errors.New(
			"direct config relation source must be inspectable and contain no inline credentials",
		)
	}
	if !source.ControlFree() {
		return RemovalPlan{}, errors.New(
			"direct config relation source must not contain control characters",
		)
	}

	switch {
	case input.Target == target.TargetOpenCode &&
		input.Carrier == desiredextension.CarrierOpenCodePlugin:
		if _, err := desiredextension.NewCarrierKey(
			input.Carrier,
			input.Target,
			input.Scope,
			source,
		); err != nil {
			return RemovalPlan{}, err
		}
		paths, err := selectOpenCodeConfigPaths(
			input.AuthorityPaths,
			input.Scope,
		)
		if err != nil {
			return RemovalPlan{}, err
		}
		return RemovalPlan{
			kind:   removalKindOpenCodePlugin,
			target: input.Target,
			scope:  input.Scope,
			source: source.Ref(),
			paths:  paths,
		}, nil
	default:
		return RemovalPlan{}, fmt.Errorf(
			"target %q carrier %q has no admitted direct config relation remover",
			input.Target,
			input.Carrier,
		)
	}
}

// PhysicalAuthority returns the complete path authority required by the plan.
func (plan RemovalPlan) PhysicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	if err := plan.validate(); err != nil {
		return mutation.PhysicalAuthoritySet{}, err
	}
	requests := make([]mutation.PhysicalAuthorityRequest, 0, len(plan.paths))
	for _, path := range plan.paths {
		requests = append(requests, mutation.PhysicalAuthorityRequest{
			Path:   path,
			Target: string(plan.target),
			Scope:  string(plan.scope),
		})
	}
	return mutation.NewPhysicalAuthoritySet(requests...)
}

// Bind captures retained-root entry authority for every selected document.
func (plan RemovalPlan) Bind(
	selectedRoot *rootedpath.CapturedRoot,
	selectedRootPath string,
) (*BoundRemoval, error) {
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
				fmt.Errorf("bind direct config relation document[%d]: %w", index, err),
				closeAuthorities(authorities),
			)
		}
		authorities = append(authorities, authority)
	}
	return &BoundRemoval{plan: plan, authorities: authorities}, nil
}

func (plan RemovalPlan) validate() error {
	if plan.kind != removalKindOpenCodePlugin {
		return fmt.Errorf("direct config relation removal kind is invalid")
	}
	if plan.target != target.TargetOpenCode ||
		plan.scope == "" ||
		plan.source == "" ||
		len(plan.paths) != 4 {
		return fmt.Errorf("direct config relation removal plan is incomplete")
	}
	return nil
}

// BoundRemoval owns entry bindings for one direct mutation attempt.
type BoundRemoval struct {
	plan        RemovalPlan
	authorities []*rootedpath.EntryAuthority
	closed      bool
}

// Execute removes the exact selected relation from every current authority
// document. Earlier document commits remain durable if a later document fails.
func (removal *BoundRemoval) Execute(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
) (changedDocuments int, resultErr error) {
	if removal == nil || removal.closed {
		return 0, fmt.Errorf("bound direct config relation removal is unavailable")
	}
	if err := removal.plan.validate(); err != nil {
		return 0, err
	}
	if len(removal.authorities) != len(removal.plan.paths) {
		return 0, fmt.Errorf("bound direct config relation authority is incomplete")
	}
	for index, authority := range removal.authorities {
		changed, err := removeOpenCodeExactSource(
			ctx,
			filesystem,
			authority,
			removal.plan.source,
		)
		if err != nil {
			return changedDocuments, fmt.Errorf(
				"remove direct config relation from selected document[%d]: %w",
				index,
				err,
			)
		}
		if changed {
			changedDocuments++
		}
	}
	return changedDocuments, nil
}

// Close releases every independently captured entry root.
func (removal *BoundRemoval) Close() error {
	if removal == nil || removal.closed {
		return nil
	}
	removal.closed = true
	err := closeAuthorities(removal.authorities)
	removal.authorities = nil
	return err
}

func selectOpenCodeConfigPaths(
	authorityPaths []observerelation.AuthorityPath,
	scope target.Scope,
) ([]string, error) {
	byName := make(map[string]string, 4)
	root := ""
	for _, authorityPath := range authorityPaths {
		if authorityPath.Target() != target.TargetOpenCode ||
			authorityPath.Scope() != scope {
			continue
		}
		name := filepath.Base(authorityPath.Path())
		_, admitted := openCodeConfigKindForName(name)
		if !admitted {
			return nil, fmt.Errorf(
				"OpenCode relation authority path %q is not a selected config document",
				authorityPath.Path(),
			)
		}
		if _, duplicate := byName[name]; duplicate {
			return nil, fmt.Errorf(
				"OpenCode relation authority contains duplicate config candidate %q",
				name,
			)
		}
		candidateRoot := filepath.Dir(authorityPath.Path())
		if root != "" && candidateRoot != root {
			return nil, fmt.Errorf("OpenCode config candidates do not share one root")
		}
		root = candidateRoot
		byName[name] = authorityPath.Path()
	}

	paths := make([]string, 0, 4)
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		names, err := opencodeconfig.CandidateNames(kind)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			path, present := byName[name]
			if !present {
				return nil, fmt.Errorf(
					"OpenCode direct removal requires config candidate authority %q",
					name,
				)
			}
			paths = append(paths, path)
		}
	}
	return paths, nil
}

func openCodeConfigKindForName(name string) (opencodeconfig.ConfigKind, bool) {
	for _, kind := range []opencodeconfig.ConfigKind{
		opencodeconfig.ConfigServer,
		opencodeconfig.ConfigTUI,
	} {
		candidates, err := opencodeconfig.CandidateNames(kind)
		if err != nil {
			panic(err)
		}
		if slices.Contains(candidates, name) {
			return kind, true
		}
	}
	return "", false
}

func removeOpenCodeExactSource(
	ctx context.Context,
	filesystem mutationfs.RootedStore,
	authority *rootedpath.EntryAuthority,
	source string,
) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("OpenCode relation removal context is required")
	}
	if filesystem == nil {
		return false, fmt.Errorf("OpenCode relation removal filesystem is required")
	}
	capability, err := authority.Acquire()
	if err != nil {
		return false, err
	}
	content, mode, identity, err := filesystem.ReadRootedRegularFileUpTo(
		ctx,
		capability,
		maximumConfigBytes,
	)
	if errors.Is(err, os.ErrNotExist) {
		return false, capability.Close()
	}
	if err != nil {
		return false, errors.Join(err, capability.Close())
	}

	parseContent := content
	if len(bytes.TrimSpace(parseContent)) == 0 {
		parseContent = []byte("{}")
	}
	document, err := opencodeconfig.Parse(parseContent)
	if err != nil {
		return false, errors.Join(err, capability.Close())
	}
	candidate, changed, err := document.RemoveExactSource(source)
	if err != nil {
		return false, errors.Join(err, capability.Close())
	}
	if !changed {
		return false, capability.Close()
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
	return true, nil
}

func closeAuthorities(authorities []*rootedpath.EntryAuthority) error {
	var result error
	for index := len(authorities) - 1; index >= 0; index-- {
		result = errors.Join(result, authorities[index].Close())
	}
	return result
}
