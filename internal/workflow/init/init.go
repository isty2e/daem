package initworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"

	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/operationplan"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

const (
	ActionCreate    = "create"
	ActionOverwrite = "overwrite"
)

type Input struct {
	ManifestPath string
	Force        bool
}

type Plan struct {
	ManifestPath string
	Content      []byte
	Action       string
}

func BuildPlan(ctx context.Context, input Input) (Plan, error) {
	paths, err := daempaths.ResolveCreation(input.ManifestPath)
	if err != nil {
		return Plan{}, err
	}
	if err := recoverygate.RequireFileSetClear(ctx, paths.StateDir); err != nil {
		return Plan{}, err
	}

	content := declarationmanifest.StarterContent()
	if _, err := declarationmanifest.Decode(content); err != nil {
		return Plan{}, fmt.Errorf("init template is invalid: %w", err)
	}

	action := ActionCreate
	_, err = declarationartifact.Read(ctx, paths.ManifestPath)
	if err == nil {
		if !input.Force {
			return Plan{}, fmt.Errorf("manifest already exists: %s; use --force to overwrite", paths.ManifestPath)
		}
		action = ActionOverwrite
	} else if !os.IsNotExist(err) {
		return Plan{}, fmt.Errorf("check manifest path: %w", err)
	}

	return Plan{
		ManifestPath: paths.ManifestPath,
		Content:      content,
		Action:       action,
	}, nil
}

// Execute builds and writes an init plan while holding its complete mutation authority.
func Execute(ctx context.Context, input Input) (result Plan, returnErr error) {
	if ctx == nil {
		return Plan{}, fmt.Errorf("init context is required")
	}
	paths, err := daempaths.ResolveCreation(input.ManifestPath)
	if err != nil {
		return Plan{}, err
	}
	barrier, err := recoverygate.NewEffectAuthority(ctx, paths)
	if err != nil {
		return Plan{}, err
	}
	metadataTransactionPath, err := fileset.FileSetAuthorityPath(paths.StateDir)
	if err != nil {
		return Plan{}, err
	}
	program := operationplan.CompileInit(operationplan.InitInput{
		ManifestPath:            paths.ManifestPath,
		ManifestMaximumBytes:    declarationartifact.MaximumBytes,
		MetadataTransactionPath: metadataTransactionPath,
		BarrierDomains:          barrier.Domains(),
		BarrierRevisions:        barrier.RevisionRequests(),
	})
	domains, err := lowerInitDomainSteps(program.DomainSteps())
	if err != nil {
		return Plan{}, err
	}
	store, err := mutation.NewStore(paths.DataDir)
	if err != nil {
		return Plan{}, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return Plan{}, err
	}
	defer func() {
		if err := leases.Release(); err != nil {
			returnErr = errors.Join(returnErr, err)
		}
	}()
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return Plan{}, err
	} else if !matches {
		return Plan{}, mutation.StaleSnapshotError{}
	}
	if err := barrier.Validate(ctx); err != nil {
		return Plan{}, err
	}

	revisionRequests, err := program.RevisionRequests()
	if err != nil {
		return Plan{}, err
	}
	revisions, err := mutation.CaptureRevisionSet(ctx, revisionRequests...)
	if err != nil {
		return Plan{}, err
	}
	plan, err := BuildPlan(ctx, input)
	if err != nil {
		return Plan{}, err
	}
	matches, err := revisions.MatchesCurrent(ctx)
	if err != nil {
		return Plan{}, err
	}
	if !matches {
		return Plan{}, mutation.StaleSnapshotError{}
	}
	if matches, err := leases.DomainsMatchCurrent(ctx); err != nil {
		return Plan{}, err
	} else if !matches {
		return Plan{}, mutation.StaleSnapshotError{}
	}
	if err := barrier.Validate(ctx); err != nil {
		return Plan{}, err
	}
	if err := writePlan(ctx, plan, input.Force); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func lowerInitDomainSteps(steps []operationplan.DomainStep) ([]mutation.Domain, error) {
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
		if domain, ok := step.Compiled(); ok {
			domains = append(domains, domain)
			continue
		}
		request, ok := step.Path()
		if !ok {
			return nil, fmt.Errorf("init operation domain step is invalid")
		}
		if logical, logicalPath := request.Logical(); logicalPath {
			domain, err := mutation.NewLogicalPathDomain(logical)
			if err != nil {
				return nil, err
			}
			domains = append(domains, domain)
			continue
		}
		return nil, fmt.Errorf("init operation path-domain request is not logical")
	}
	return domains, nil
}

func writePlan(ctx context.Context, plan Plan, force bool) error {
	if ctx == nil {
		return fmt.Errorf("init context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	commitPath, err := mutation.CanonicalDirectoryEntryPath(plan.ManifestPath)
	if err != nil {
		return fmt.Errorf("canonicalize manifest commit path: %w", err)
	}
	if force {
		if info, err := os.Stat(plan.ManifestPath); err == nil {
			expected, err := storagecommit.CaptureEntryIdentity(ctx, commitPath)
			if err != nil {
				return fmt.Errorf("capture manifest identity: %w", err)
			}
			request, err := storagecommit.NewFileReplacement(
				commitPath,
				plan.Content,
				info.Mode().Perm(),
				expected,
			)
			if err != nil {
				return fmt.Errorf("prepare manifest replacement: %w", err)
			}
			if err := storagecommit.CommitFile(ctx, request); err != nil {
				return fmt.Errorf("commit manifest replacement: %w", err)
			}
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
	}

	request, err := storagecommit.NewFileCreate(commitPath, plan.Content, 0o600)
	if err != nil {
		return fmt.Errorf("prepare manifest creation: %w", err)
	}
	if err := storagecommit.CommitFile(ctx, request); err != nil {
		return fmt.Errorf("commit manifest creation: %w", err)
	}
	return nil
}
