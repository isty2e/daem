package codexplugin

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	"github.com/isty2e/daem/internal/filesnapshot"
)

type childKind uint8

const (
	childMissing childKind = iota
	childSymlink
	childDirectory
	childFile
	childOther
)

type observationDir struct {
	file *os.File
}

func (dir *observationDir) close() {
	if dir == nil || dir.file == nil {
		return
	}
	_ = dir.file.Close()
	dir.file = nil
}

type snapshotRecord struct {
	content []byte
	exists  bool
	reason  observecontribution.SourceContributionReason
	err     error
}

type pluginObservation struct {
	dir       *observationDir
	budget    *observationBudget
	snapshots map[string]snapshotRecord
}

func openPluginCacheLayout(path string, budget *observationBudget) (*pluginObservation, observecontribution.SourceContributionReason, error) {
	file, err := openDirectory(path)
	if err != nil {
		if directoryMissing(err) {
			return nil, observecontribution.SourceContributionReasonNone, nil
		}
		reason, err := classifyDirectoryError(err)
		return nil, reason, err
	}
	return newPluginObservation(file, budget), observecontribution.SourceContributionReasonNone, nil
}

func newPluginObservation(file *os.File, budget *observationBudget) *pluginObservation {
	return &pluginObservation{
		dir:       &observationDir{file: file},
		budget:    budget,
		snapshots: map[string]snapshotRecord{},
	}
}

func (observation *pluginObservation) close() {
	if observation == nil {
		return
	}
	observation.dir.close()
}

func (observation *pluginObservation) openChildDirectory(name string) (*pluginObservation, observecontribution.SourceContributionReason, error) {
	if observation == nil || observation.dir == nil || observation.dir.file == nil {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
	}
	if !validDirentComponent(name) {
		return nil, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	file, err := openChildDirectoryNoFollow(observation.dir.file, name)
	if err != nil {
		reason, err := classifyDirectoryError(err)
		return nil, reason, err
	}
	return newPluginObservation(file, observation.budget), observecontribution.SourceContributionReasonNone, nil
}

func (observation *pluginObservation) listNames(
	ctx context.Context,
) ([]string, observecontribution.SourceContributionReason, error) {
	if observation == nil || observation.dir == nil || observation.dir.file == nil {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
	}
	if ctx == nil {
		return nil, observecontribution.SourceContributionReasonNone, errors.New("Codex plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, observecontribution.SourceContributionReasonNone, err
	}
	if observation.budget == nil || observation.budget.exceeded {
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	remaining := observation.budget.remainingEntries()
	if remaining == 0 {
		observation.budget.exhaust()
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	names, err := readDirectoryNamesFrom(ctx, observation.dir.file, remaining)
	if err != nil {
		reason, err := classifyDirectoryError(err)
		return nil, reason, err
	}
	if len(names) > remaining || observation.budget.consumeNames(names) {
		observation.budget.exhaust()
		return nil, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	return names, observecontribution.SourceContributionReasonNone, nil
}

func (observation *pluginObservation) snapshot(
	ctx context.Context,
	relative string,
) ([]byte, bool, observecontribution.SourceContributionReason, error) {
	relative = strings.Trim(relative, "/")
	if relative == "" {
		return nil, false, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	if cached, ok := observation.snapshots[relative]; ok {
		return cached.content, cached.exists, cached.reason, cached.err
	}
	content, exists, reason, err := observation.snapshotUncached(ctx, relative)
	observation.snapshots[relative] = snapshotRecord{content: content, exists: exists, reason: reason, err: err}
	return content, exists, reason, err
}

func (observation *pluginObservation) requiredFile(
	ctx context.Context,
	relative string,
) ([]byte, observecontribution.SourceContributionReason, error) {
	content, exists, reason, err := observation.snapshot(ctx, relative)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, reason, err
	}
	if !exists {
		return nil, observecontribution.SourceContributionReasonArtifactUnavailable, nil
	}
	return content, observecontribution.SourceContributionReasonNone, nil
}

func (observation *pluginObservation) classify(
	relative string,
) (childKind, observecontribution.SourceContributionReason, error) {
	parent, name, closer, reason, err := observation.openParent(relative)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return childMissing, reason, err
	}
	defer closer()
	kind, err := classifyChild(parent, name)
	if err != nil {
		reason, err := classifyDirectoryError(err)
		return childMissing, reason, err
	}
	if kind == childSymlink {
		return kind, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	return kind, observecontribution.SourceContributionReasonNone, nil
}

func (observation *pluginObservation) snapshotUncached(
	ctx context.Context,
	relative string,
) ([]byte, bool, observecontribution.SourceContributionReason, error) {
	if ctx == nil {
		return nil, false, observecontribution.SourceContributionReasonNone, errors.New("Codex plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, observecontribution.SourceContributionReasonNone, err
	}
	if observation.budget == nil || observation.budget.exceeded {
		return nil, false, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	limit := observation.budget.snapshotLimit()
	if limit <= 0 {
		observation.budget.exhaust()
		return nil, false, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	parent, name, closer, reason, err := observation.openParent(relative)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, false, reason, err
	}
	defer closer()
	content, exists, err := filesnapshot.ReadRegularFileAt(ctx, parent, name, limit)
	if err != nil {
		return nil, false, classifySnapshotError(err), snapshotObservationError(err)
	}
	if exists && observation.budget.consumeSnapshotBytes(int64(len(content))) {
		return nil, false, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	return content, exists, observecontribution.SourceContributionReasonNone, nil
}

func (observation *pluginObservation) openParent(
	relative string,
) (parent *os.File, name string, closer func(), reason observecontribution.SourceContributionReason, err error) {
	parts := strings.Split(relative, "/")
	if len(parts) > MaximumObservationPathComponents {
		return nil, "", func() {}, observecontribution.SourceContributionReasonArtifactBudgetExceeded, nil
	}
	for _, part := range parts {
		if !validDirentComponent(part) {
			return nil, "", func() {}, observecontribution.SourceContributionReasonArtifactPathBlocked, nil
		}
	}
	name = parts[len(parts)-1]
	parent = observation.dir.file
	var owned *os.File
	closer = func() {
		if owned != nil {
			_ = owned.Close()
			owned = nil
		}
	}
	for _, part := range parts[:len(parts)-1] {
		child, openErr := openChildDirectoryNoFollow(parent, part)
		closer()
		if openErr != nil {
			reason, err := classifyDirectoryError(openErr)
			return nil, "", func() {}, reason, err
		}
		owned = child
		parent = child
	}
	return parent, name, closer, observecontribution.SourceContributionReasonNone, nil
}

func validDirentComponent(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	return !strings.ContainsRune(name, '/') && !strings.ContainsRune(name, '\\') && !strings.ContainsRune(name, 0)
}

func classifyDirectoryError(err error) (observecontribution.SourceContributionReason, error) {
	if err == nil {
		return observecontribution.SourceContributionReasonNone, nil
	}
	if observationCanceled(err) {
		return observecontribution.SourceContributionReasonNone, err
	}
	if directoryMissing(err) {
		return observecontribution.SourceContributionReasonArtifactUnavailable, err
	}
	if directoryPathBlocked(err) {
		return observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	if errors.Is(err, filesnapshot.ErrSymlink) {
		return observecontribution.SourceContributionReasonArtifactPathBlocked, nil
	}
	return observecontribution.SourceContributionReasonArtifactUnavailable, nil
}

func readDirectoryNamesFrom(ctx context.Context, file *os.File, maximumEntries int) ([]string, error) {
	if ctx == nil {
		return nil, errors.New("Codex plugin observation context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if file == nil {
		return nil, errors.New("Codex plugin directory descriptor is required")
	}
	if maximumEntries < 0 {
		return nil, errors.New("Codex plugin directory listing budget is required")
	}

	const batchMaximum = 256
	names := make([]string, 0, min(maximumEntries+1, batchMaximum))
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		remaining := maximumEntries + 1 - len(names)
		if remaining <= 0 {
			break
		}
		batch, err := file.Readdirnames(min(batchMaximum, remaining))
		names = append(names, batch...)
		if err := ctx.Err(); err != nil {
			readErr = err
			break
		}
		if len(names) > maximumEntries {
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			readErr = err
			break
		}
		if len(batch) == 0 {
			readErr = errors.New("Codex plugin directory enumeration made no progress")
			break
		}
	}
	if readErr != nil {
		return nil, readErr
	}
	return names, nil
}
