package migrate

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/assurance/stateauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	"github.com/isty2e/daem/internal/operationplan"
	ownershipstore "github.com/isty2e/daem/internal/output/ownership/store"
)

func (prepared *PreparedState) Execute(ctx context.Context) (result StateDisclosure, returnErr error) {
	prepared.mu.Lock()
	if prepared.closed || prepared.paths.StateDir == "" {
		prepared.mu.Unlock()
		return StateDisclosure{}, fmt.Errorf("state migration preparation is not available")
	}
	prepared.closed = true
	prepared.mu.Unlock()
	if ctx == nil {
		return StateDisclosure{}, fmt.Errorf("state migration context is required")
	}
	if prepared.disclosure.Action == "already_migrated" {
		if err := prepared.validateRevisions(ctx); err != nil {
			return StateDisclosure{}, err
		}
		return prepared.disclosure, nil
	}
	domains, err := prepared.domains()
	if err != nil {
		return StateDisclosure{}, err
	}
	store, err := mutation.NewStore(prepared.paths.DataDir)
	if err != nil {
		return StateDisclosure{}, err
	}
	leases, err := store.Acquire(ctx, domains...)
	if err != nil {
		return StateDisclosure{}, err
	}
	defer func() { returnErr = errors.Join(returnErr, leases.Release()) }()
	if err := prepared.validateExecution(ctx, leases); err != nil {
		return StateDisclosure{}, err
	}
	if prepared.input.Recover {
		action, err := fileset.InspectFileSetRecovery(ctx, prepared.paths.StateDir, prepared.targetPaths())
		if err != nil {
			return StateDisclosure{}, err
		}
		if string(action) != prepared.disclosure.Action {
			return StateDisclosure{}, mutation.StaleSnapshotError{}
		}
		if err := fileset.RecoverFileSet(ctx, prepared.paths.StateDir, prepared.targetPaths()); err != nil {
			return StateDisclosure{}, err
		}
		return prepared.disclosure, nil
	}
	_, err = prepared.targetBarrier.EnsureStateDirForEffect(ctx, func(ctx context.Context, created bool) error {
		if created {
			if err := prepared.validateRevisions(ctx); err != nil {
				return err
			}
			accepted, err := leases.AcceptVisibilityChanges(ctx)
			if err != nil {
				return err
			}
			if !accepted {
				return fmt.Errorf("accept destination namespace after owned creation: %w", mutation.StaleSnapshotError{})
			}
		}
		return prepared.validateExecution(ctx, leases)
	})
	if err != nil {
		return StateDisclosure{}, err
	}
	to, err := authorityFor(prepared.paths)
	if err != nil {
		return StateDisclosure{}, err
	}
	targets, err := prepared.afterImages(to)
	if err != nil {
		return StateDisclosure{}, err
	}
	if err := prepared.validateExecution(ctx, leases); err != nil {
		return StateDisclosure{}, err
	}
	if err := fileset.CommitFileSet(ctx, fileset.FileSetInput{StateDir: prepared.paths.StateDir, Targets: targets}); err != nil {
		return StateDisclosure{}, err
	}
	result = prepared.disclosure
	result.Action = "migrated"
	return result, nil
}

func (prepared *PreparedState) validateExecution(ctx context.Context, leases *mutation.LeaseSet) error {
	if err := prepared.validateRevisions(ctx); err != nil {
		return fmt.Errorf("state migration input revisions: %w", err)
	}
	matches, err := leases.DomainsMatchCurrent(ctx)
	if err != nil {
		return err
	}
	if !matches {
		return fmt.Errorf("state migration lease domains: %w", mutation.StaleSnapshotError{})
	}
	if err := prepared.sourceBarrier.Validate(ctx); err != nil {
		return err
	}
	if prepared.input.Recover {
		return prepared.targetBarrier.ValidateFileSetRecovery(ctx)
	}
	return prepared.targetBarrier.Validate(ctx)
}

func (prepared *PreparedState) domains() ([]mutation.Domain, error) {
	marker, err := fileset.FileSetAuthorityPath(prepared.paths.StateDir)
	if err != nil {
		return nil, err
	}
	steps := operationplan.CompileMetadataDomains(operationplan.MetadataDomainInput{
		TargetPaths: prepared.targetPaths(), MarkerPath: marker,
		TrailingDomains: append(prepared.sourceBarrier.Domains(), prepared.targetBarrier.Domains()...),
	})
	domains := make([]mutation.Domain, 0, len(steps))
	for _, step := range steps {
		if compiled, ok := step.Compiled(); ok {
			domains = append(domains, compiled)
			continue
		}
		request, ok := step.Path()
		if !ok {
			return nil, fmt.Errorf("state migration domain step is invalid")
		}
		logical, ok := request.Logical()
		if !ok {
			return nil, fmt.Errorf("state migration domain is not logical")
		}
		// All targets are metadata writes, not declaration symlinks. Their
		// referents need exclusive namespace coverage for owned root creation.
		if logical.Effect == mutation.PathEffectReferent {
			logical.Access = mutation.AccessExclusive
		}
		domain, err := mutation.NewLogicalPathDomain(logical)
		if err != nil {
			return nil, err
		}
		domains = append(domains, domain)
	}
	return domains, nil
}

func (prepared *PreparedState) afterImages(to stateauthority.Authority) ([]fileset.FileTarget, error) {
	snapshot, err := prepared.snapshot.TransferAuthority(prepared.from, to)
	if err != nil {
		return nil, err
	}
	outputs, err := prepared.outputs.TransferAuthority(prepared.from, to)
	if err != nil {
		return nil, err
	}
	carriers, err := prepared.carriers.TransferAuthority(prepared.from, to)
	if err != nil {
		return nil, err
	}
	stateContent, err := statefile.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	outputContent, err := ownershipstore.Marshal(outputs)
	if err != nil {
		return nil, err
	}
	carrierContent, err := carrierclaim.Marshal(carriers)
	if err != nil {
		return nil, err
	}
	receipt, err := statefile.NewRelocation(prepared.from, to)
	if err != nil {
		return nil, err
	}
	receiptContent, err := statefile.MarshalRelocation(receipt)
	if err != nil {
		return nil, err
	}
	targets := make([]fileset.FileTarget, 0, 4)
	for _, image := range []struct {
		path    string
		content []byte
	}{
		{prepared.paths.StatefilePath, stateContent},
		{prepared.paths.OwnershipRegistryPath, outputContent},
		{prepared.paths.CarrierClaimRegistryPath, carrierContent},
	} {
		target, err := fileset.NewFileWrite(image.path, image.content)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	retired, err := fileset.NewFileCommitPointWrite(prepared.legacy.StatefilePath, receiptContent)
	if err != nil {
		return nil, err
	}
	return append(targets, retired), nil
}
