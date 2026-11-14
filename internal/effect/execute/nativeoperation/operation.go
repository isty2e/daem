package nativeoperation

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/controlwitness"
	"github.com/isty2e/daem/internal/effect/execute/hostmutation"
	"github.com/isty2e/daem/internal/effect/execute/mutationnamespace"
	"github.com/isty2e/daem/internal/effect/execute/removaloperation"
	"github.com/isty2e/daem/internal/effect/execute/statefileoperation"
	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"
	"github.com/isty2e/daem/internal/effect/mutation"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	ownershipmutation "github.com/isty2e/daem/internal/effect/mutation/ownership"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/target"
)

type operation struct {
	destinations               mutationnamespace.Namespace
	control                    *controlwitness.Guard
	removals                   *removaloperation.Operation
	filesystem                 mutationfs.Store
	capturedRoot               *rootedpath.CapturedRoot
	projectStatefile           *statefileoperation.Selected
	recoveryJournal            *rootedpath.EntryAuthority
	recoveryJournalRecord      *rootedpath.EntryAuthority
	journalBasis               journalinventory.ExecutionBasis
	physicalAuthorityRequests  []mutation.PhysicalAuthorityRequest
	physicalWorkBudget         *physicalwork.Budget
	generalTraversalPhase      *physicalwork.TraversalPhase
	recoveryBackups            *hostmutation.Backups
	generalExecutionWorkBudget *physicalwork.Budget
	ownerReference             pathauthority.Reference
}

func newOperation(paths execute.Paths, borrowedRoot *rootedpath.CapturedRoot, resolver mutationnamespace.Resolver, filesystem mutationfs.Store, budget *physicalwork.Budget) (*operation, error) {
	if resolver == nil {
		return nil, fmt.Errorf("mutation destination resolver is required")
	}
	if filesystem == nil {
		return nil, fmt.Errorf("mutation filesystem is required")
	}
	if budget == nil {
		return nil, fmt.Errorf("mutation physical work budget is required")
	}
	phase, err := physicalwork.NewTraversalPhase(budget)
	if err != nil {
		return nil, err
	}
	destinations, err := mutationnamespace.New(resolver, phase)
	if err != nil {
		return nil, err
	}
	root, err := rootedpath.CaptureRootBounded(paths.ManifestRoot, physicalwork.MaximumPhysicalPathDepth, phase)
	if err != nil {
		return nil, fmt.Errorf("capture bounded project mutation root: %w", err)
	}
	if err := destinations.BindProject(root, borrowedRoot); err != nil {
		_ = root.Close()
		return nil, err
	}

	control := controlwitness.New(filesystem)
	return &operation{
		destinations: destinations, control: control, filesystem: filesystem, capturedRoot: root,
		physicalWorkBudget: budget, generalTraversalPhase: phase,
		removals: removaloperation.New(destinations, filesystem, control, budget, phase),
	}, nil
}

func (operation *operation) hostMutation() *hostmutation.Operation {
	return hostmutation.New(operation.destinations, operation.filesystem, operation.removals)
}

func (operation *operation) hostBackups() *hostmutation.Backups {
	if operation.recoveryBackups == nil {
		operation.recoveryBackups = hostmutation.NewBackups(operation.hostMutation(), operation.physicalWorkBudget)
	}
	return operation.recoveryBackups
}

func (operation *operation) bindOwnershipRegistry(path string, binder ownershipmutation.RootedRegistryBinder) (ownershipmutation.RegistryStore, error) {
	if operation.physicalWorkBudget == nil {
		return nil, fmt.Errorf("mutation physical work budget is required")
	}
	if binder == nil {
		return nil, fmt.Errorf("rooted ownership registry binder is required")
	}
	root, destination, err := rootedpath.CaptureDestinationBounded(path, physicalwork.MaximumPhysicalPathDepth, operation.generalTraversalPhase)
	if err != nil {
		return nil, fmt.Errorf("capture ownership registry authority: %w", err)
	}
	root, err = operation.destinations.RetainRoot(root, destination)
	if err != nil {
		return nil, err
	}
	registry, err := binder(root, destination, physicalwork.MaximumPhysicalPathDepth, operation.generalTraversalPhase)
	if err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, fmt.Errorf("rooted ownership registry binder returned a nil store")
	}
	if err := operation.control.BindOwnership(root, destination, registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func (operation *operation) bindProjectStatefile(selectedRoot, path string) error {
	if operation.projectStatefile != nil {
		return fmt.Errorf("project statefile authority is already bound")
	}
	destination, err := operation.bindProjectControlEntry(selectedRoot, path)
	if err != nil {
		return fmt.Errorf("bind project statefile: %w", err)
	}
	operation.projectStatefile = statefileoperation.NewSelected(operation.filesystem, destination, operation.physicalWorkBudget)
	return nil
}

func (operation *operation) bindRecoveryJournal(selectedRoot, directory string) error {
	if operation.recoveryJournal != nil || operation.recoveryJournalRecord != nil {
		return fmt.Errorf("recovery journal authority is already bound")
	}
	bound, err := operation.bindProjectControlEntry(selectedRoot, directory)
	if err != nil {
		return fmt.Errorf("bind recovery journal: %w", err)
	}
	record, err := operation.bindProjectControlEntry(selectedRoot, filepath.Join(directory, "journal.json"))
	if err != nil {
		return errors.Join(fmt.Errorf("bind recovery journal record: %w", err), bound.Close())
	}
	operation.recoveryJournal, operation.recoveryJournalRecord = bound, record
	return nil
}

func (operation *operation) bindProjectControlEntry(selectedRoot, path string) (*rootedpath.EntryAuthority, error) {
	if operation.generalTraversalPhase == nil {
		return nil, fmt.Errorf("project control traversal authority is unavailable")
	}
	return rootedpath.BindSelectedControlEntryAuthorityBounded(operation.capturedRoot, selectedRoot, path, physicalwork.MaximumPhysicalPathDepth, operation.generalTraversalPhase)
}

func (operation *operation) setJournalExecutionBasis(fingerprint string, active journalinventory.ActiveJournalAuthority) error {
	basis, err := journalinventory.NewExecutionBasis(fingerprint, active)
	if err != nil {
		return err
	}
	operation.journalBasis = basis
	return nil
}

func (operation *operation) validateProjectSelection(selectedRoot string) error {
	if operation.capturedRoot == nil {
		return fmt.Errorf("project mutation authority is unavailable")
	}
	return operation.capturedRoot.ValidateSelectionBounded(selectedRoot, physicalwork.MaximumPhysicalPathDepth, operation.generalTraversalPhase)
}

func (operation *operation) bindPhysicalAuthority(scope target.Scope, destination output.Destination, consumers []target.Target) error {
	requests, err := operation.destinations.BindPhysicalAuthority(scope, destination, consumers)
	operation.physicalAuthorityRequests = append(operation.physicalAuthorityRequests, requests...)
	return err
}

func (operation *operation) physicalAuthority() (mutation.PhysicalAuthoritySet, error) {
	return mutation.NewPhysicalAuthoritySet(operation.physicalAuthorityRequests...)
}

func (operation *operation) close() error {
	if operation == nil {
		return nil
	}
	result := operation.destinations.Close()
	if err := operation.projectStatefile.Close(); err != nil {
		result = errors.Join(result, fmt.Errorf("close project statefile authority: %w", err))
	}
	operation.projectStatefile = nil
	if err := operation.recoveryJournal.Close(); err != nil {
		result = errors.Join(result, fmt.Errorf("close recovery journal authority: %w", err))
	}
	operation.recoveryJournal = nil
	if err := operation.recoveryJournalRecord.Close(); err != nil {
		result = errors.Join(result, fmt.Errorf("close recovery journal record authority: %w", err))
	}
	operation.recoveryJournalRecord = nil
	operation.journalBasis = journalinventory.ExecutionBasis{}
	operation.physicalWorkBudget = nil
	operation.recoveryBackups = nil
	operation.generalExecutionWorkBudget = nil
	operation.removals = nil
	operation.generalTraversalPhase = nil
	operation.filesystem = nil
	if operation.capturedRoot == nil {
		return result
	}
	err := operation.capturedRoot.Close()
	operation.capturedRoot = nil
	return errors.Join(result, err)
}
