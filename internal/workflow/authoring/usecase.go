// Package authoring owns manifest authoring use cases and declaration add,
// remove, target-merge, and target-narrowing operations over manifest
// declaration codecs.
//
// Declaration syntax scanning/rendering lives in internal/declaration/codec and
// byte-preserving document edits live in internal/declaration. This package
// delegates prospective snapshot generation to lock/generate when an authoring command
// needs to lock prospective manifest bytes. Atomic manifest+lockfile file
// writes live in internal/declaration/transaction. This package does not
// own diagnostics, presentation, or CLI output.
//
// Skill groups are declaration-generator syntax, not canonical resources. They
// use this package for append and ordinary target edits, but member deletion is
// a documented generator-specific exception because one declaration block can
// name multiple future skills.
package authoring

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/declaration"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/declaration/transaction"
	"github.com/isty2e/daem/internal/declarationartifact"
	daempaths "github.com/isty2e/daem/internal/paths"
)

// AuthoringMode selects whether an authoring operation only plans changes or writes them.
type AuthoringMode string

const (
	// AuthoringModeDryRun builds the prospective manifest and lockfile without writing.
	AuthoringModeDryRun AuthoringMode = "dry-run"
	// AuthoringModeWrite writes the manifest and lockfile as one authoring transaction.
	AuthoringModeWrite AuthoringMode = "write"
)

// ExecutionOptions are command-independent facts needed to execute one authoring operation.
type ExecutionOptions struct {
	ManifestPath string
	LockfilePath string
	Mode         AuthoringMode
}

// ManifestDocument is the caller-provided manifest content to edit.
type ManifestDocument struct {
	Path    string
	Root    string
	Paths   daempaths.Paths
	Content []byte
}

// LoadManifestDocument resolves and reads a manifest document for authoring changes.
func LoadManifestDocument(ctx context.Context, manifestPath string) (ManifestDocument, error) {
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		return ManifestDocument{}, err
	}
	return loadManifestDocument(ctx, paths)
}

func loadManifestDocument(ctx context.Context, paths daempaths.Paths) (ManifestDocument, error) {
	content, err := declarationartifact.Read(ctx, paths.ManifestPath)
	if err != nil {
		return ManifestDocument{}, fmt.Errorf("read manifest: %w", err)
	}
	return ManifestDocument{
		Path:    paths.ManifestPath,
		Root:    paths.ManifestRoot,
		Paths:   paths,
		Content: content,
	}, nil
}

// Change is one validated prospective manifest edit.
type Change struct {
	ManifestPath  string
	Original      []byte
	Content       []byte
	ResourceID    string
	ChangeKind    string
	ManifestBlock string
	Warnings      []string
}

func (document ManifestDocument) validateOriginal() error {
	if _, err := declarationmanifest.Decode(document.Content); err != nil {
		return fmt.Errorf("invalid manifest: %w", err)
	}
	return nil
}

func (document ManifestDocument) validateResult(content []byte) error {
	if _, err := declarationmanifest.Decode(content); err != nil {
		return fmt.Errorf("resulting manifest is invalid: %w", err)
	}
	return nil
}

func addDeclarationChangeKind(outcome declaration.EditOutcome, appendChangeKind string, mergeChangeKind string) (string, error) {
	switch outcome {
	case declaration.EditOutcomeAppend:
		return appendChangeKind, nil
	case declaration.EditOutcomeMergeTargets:
		return mergeChangeKind, nil
	default:
		return "", fmt.Errorf("unexpected declaration edit outcome %q", outcome)
	}
}

func targetRemovalChangeKind(outcome declaration.EditOutcome, removeChangeKind string, updateChangeKind string) (string, error) {
	switch outcome {
	case declaration.EditOutcomeRemove:
		return removeChangeKind, nil
	case declaration.EditOutcomeUpdateTargets:
		return updateChangeKind, nil
	default:
		return "", fmt.Errorf("unexpected declaration edit outcome %q", outcome)
	}
}

// OperationResult contains workflow facts from one manifest+lock authoring operation.
type OperationResult struct {
	ManifestPath  string
	Original      []byte
	Content       []byte
	ResourceID    string
	ChangeKind    string
	ManifestBlock string
	Warnings      []string
	Lockfile      LockfileChange
	Mode          AuthoringMode
}

// OperationPhase identifies which phase of an authoring operation failed.
type OperationPhase string

const (
	// OperationPhaseLoadManifest means the existing manifest document could not be loaded.
	OperationPhaseLoadManifest OperationPhase = "load_manifest"
	// OperationPhaseBuildManifestChange means the resource manifest edit failed.
	OperationPhaseBuildManifestChange OperationPhase = "build_manifest_change"
	// OperationPhaseBuildLockfile means the prospective lockfile could not be built.
	OperationPhaseBuildLockfile OperationPhase = "build_lockfile"
	// OperationPhaseCommit means the manifest+lockfile transaction failed.
	OperationPhaseCommit OperationPhase = "commit"
)

// OperationError preserves the failed phase without moving CLI hint policy into workflow.
type OperationError struct {
	Phase OperationPhase
	Err   error
}

func (err OperationError) Error() string {
	if err.Err == nil {
		return string(err.Phase)
	}
	switch err.Phase {
	case OperationPhaseBuildLockfile:
		return fmt.Sprintf("lock prospective manifest: %v", err.Err)
	case OperationPhaseCommit:
		return fmt.Sprintf("write authoring transaction: %v", err.Err)
	default:
		return err.Err.Error()
	}
}

func (err OperationError) Unwrap() error {
	return err.Err
}

type authoringChangeBuilder func(ManifestDocument) (Change, error)

// AddSkill executes one skill add authoring operation.
func AddSkill(ctx context.Context, options ExecutionOptions, request AddSkillRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildAddSkillChange(document, request)
	})
}

// RemoveSkill executes one skill remove authoring operation.
func RemoveSkill(ctx context.Context, options ExecutionOptions, request RemoveSkillRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildRemoveSkillChange(document, request)
	})
}

// AddInstruction executes one instruction add authoring operation.
func AddInstruction(ctx context.Context, options ExecutionOptions, request AddInstructionRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildAddInstructionChange(document, request)
	})
}

// RemoveInstruction executes one instruction remove authoring operation.
func RemoveInstruction(ctx context.Context, options ExecutionOptions, request RemoveInstructionRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildRemoveInstructionChange(document, request)
	})
}

// AddHook executes one hook add authoring operation.
func AddHook(ctx context.Context, options ExecutionOptions, request AddHookRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildAddHookChange(document, request)
	})
}

// AddMCPServer executes one MCP server add authoring operation.
func AddMCPServer(ctx context.Context, options ExecutionOptions, request AddMCPServerRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildAddMCPServerChange(document, request)
	})
}

// AddExtension executes one extension carrier declaration add authoring operation.
func AddExtension(ctx context.Context, options ExecutionOptions, request AddExtensionRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildAddExtensionChange(document, request)
	})
}

// RemoveMCPServer executes one MCP server remove authoring operation.
func RemoveMCPServer(ctx context.Context, options ExecutionOptions, request RemoveMCPServerRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildRemoveMCPServerChange(document, request)
	})
}

// RemoveExtension executes one extension carrier declaration remove authoring operation.
func RemoveExtension(ctx context.Context, options ExecutionOptions, request RemoveExtensionRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildRemoveExtensionChange(document, request)
	})
}

// RemoveHook executes one hook remove authoring operation.
func RemoveHook(ctx context.Context, options ExecutionOptions, request RemoveHookRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildRemoveHookChange(document, request)
	})
}

// AddSkillGroup executes one skill_group add authoring operation.
func AddSkillGroup(ctx context.Context, options ExecutionOptions, request AddSkillGroupRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return BuildAddSkillGroupChange(document, request)
	})
}

func executeAuthoringOperation(ctx context.Context, options ExecutionOptions, build authoringChangeBuilder) (OperationResult, error) {
	usePersistentCache, err := options.Mode.usePersistentCache()
	if err != nil {
		return OperationResult{}, err
	}
	if ctx == nil {
		return OperationResult{}, fmt.Errorf("authoring context is required")
	}
	if options.Mode == AuthoringModeDryRun {
		if err := requireClearManifestFileSet(ctx, options.ManifestPath); err != nil {
			return OperationResult{}, OperationError{Phase: OperationPhaseLoadManifest, Err: err}
		}
	} else {
		if err := recoverAuthoringFileSetBeforeRead(ctx, options); err != nil {
			return OperationResult{}, OperationError{Phase: OperationPhaseCommit, Err: err}
		}
	}

	optimistic, err := buildAuthoringCandidate(ctx, build, options.ManifestPath)
	if err != nil {
		return OperationResult{}, err
	}
	if options.Mode == AuthoringModeWrite {
		return executeAuthoringMutation(ctx, options, build, optimistic, usePersistentCache)
	}

	lockfile, err := BuildLockfileChange(ctx, LockfileChangeInput{
		ManifestPath:       optimistic.change.ManifestPath,
		Paths:              optimistic.document.Paths,
		LockfilePath:       options.LockfilePath,
		ManifestBytes:      optimistic.change.Content,
		UsePersistentCache: usePersistentCache,
	})
	if err != nil {
		return OperationResult{}, OperationError{Phase: OperationPhaseBuildLockfile, Err: err}
	}

	return operationResultFromChange(optimistic.change, lockfile, options.Mode), nil
}

func requireClearManifestFileSet(ctx context.Context, manifestPath string) error {
	paths, err := daempaths.Resolve(manifestPath)
	if err != nil {
		return err
	}
	return transaction.RequireClearFileSet(ctx, paths.StateDir)
}

func recoverAuthoringFileSetBeforeRead(
	ctx context.Context,
	options ExecutionOptions,
) error {
	paths, err := daempaths.Resolve(options.ManifestPath)
	if err != nil {
		return err
	}
	lockfilePath := options.LockfilePath
	if lockfilePath == "" {
		lockfilePath = paths.LockfilePath
	}
	return recoverMetadataFileSetBeforeRead(
		ctx,
		paths,
		[]string{paths.ManifestPath, lockfilePath},
	)
}

func (mode AuthoringMode) usePersistentCache() (bool, error) {
	switch mode {
	case AuthoringModeDryRun:
		return false, nil
	case AuthoringModeWrite:
		return true, nil
	default:
		return false, fmt.Errorf("authoring mode must be %q or %q", AuthoringModeDryRun, AuthoringModeWrite)
	}
}

func operationResultFromChange(change Change, lockfile LockfileChange, mode AuthoringMode) OperationResult {
	return OperationResult{
		ManifestPath:  change.ManifestPath,
		Original:      append([]byte(nil), change.Original...),
		Content:       append([]byte(nil), change.Content...),
		ResourceID:    change.ResourceID,
		ChangeKind:    change.ChangeKind,
		ManifestBlock: change.ManifestBlock,
		Warnings:      append([]string(nil), change.Warnings...),
		Lockfile:      lockfile,
		Mode:          mode,
	}
}
