package transaction

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const (
	transactionVersion      = 2
	transactionDirName      = "metadata-transaction"
	transactionMarkerFile   = "transaction.json"
	maximumMarkerBytes      = 1 << 20
	maximumMarkerJSONDepth  = 64
	transactionEvidenceMode = os.FileMode(0o600)
)

type fileState struct {
	Exists     bool   `json:"exists"`
	Hash       string `json:"hash,omitempty"`
	BackupPath string `json:"backup_path,omitempty"`
	Mode       uint32 `json:"mode,omitempty"`
}

type targetMarker struct {
	Path        string    `json:"path"`
	Before      fileState `json:"before"`
	AfterHash   string    `json:"after_hash,omitempty"`
	Write       bool      `json:"write"`
	CommitPoint bool      `json:"commit_point,omitempty"`
}

type transactionMarker struct {
	Version int            `json:"version"`
	Targets []targetMarker `json:"targets"`
}

func prepareMarker(ctx context.Context, stateDir string, targets []FileTarget) (transactionMarker, error) {
	if err := ctx.Err(); err != nil {
		return transactionMarker{}, err
	}
	canonical, err := canonicalStateDir(stateDir)
	if err != nil {
		return transactionMarker{}, err
	}
	stateDir = canonical
	transactionDir := transactionDir(stateDir)
	if _, err := os.Lstat(transactionDir); err == nil {
		return transactionMarker{}, fmt.Errorf("file-set transaction evidence already exists at %s", transactionDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return transactionMarker{}, fmt.Errorf("inspect file-set transaction evidence: %w", err)
	}
	if _, err := storagecommit.PrepareCommitParent(ctx, transactionDir); err != nil {
		return transactionMarker{}, fmt.Errorf("prepare file-set evidence parent: %w", err)
	}
	stagedDir, err := os.MkdirTemp(stateDir, ".metadata-stage-")
	if err != nil {
		return transactionMarker{}, fmt.Errorf("create file-set evidence stage: %w", err)
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = os.RemoveAll(stagedDir)
		}
	}()

	marker := transactionMarker{
		Version: transactionVersion,
		Targets: make([]targetMarker, 0, len(targets)),
	}
	for index, target := range targets {
		backupName := fmt.Sprintf("target-%03d.before", index)
		before, captureErr := captureFileState(
			ctx,
			target.path,
			filepath.Join(stagedDir, backupName),
			filepath.Join(transactionDir, backupName),
		)
		if captureErr != nil {
			return transactionMarker{}, captureErr
		}
		row := targetMarker{
			Path:        target.path,
			Before:      before,
			Write:       target.write,
			CommitPoint: target.commitPoint,
		}
		if target.write {
			row.AfterHash = hashBytes(target.content)
		}
		marker.Targets = append(marker.Targets, row)
	}

	content, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return transactionMarker{}, fmt.Errorf("marshal file-set transaction marker: %w", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(filepath.Join(stagedDir, transactionMarkerFile), content, transactionEvidenceMode); err != nil {
		return transactionMarker{}, fmt.Errorf("stage file-set transaction marker: %w", err)
	}
	stagedIdentity, err := storagecommit.CaptureEntryIdentity(ctx, stagedDir)
	if err != nil {
		return transactionMarker{}, fmt.Errorf("capture file-set evidence stage identity: %w", err)
	}
	request, err := storagecommit.NewPreparedTreeCommit(stagedDir, transactionDir, stagedIdentity)
	if err != nil {
		return transactionMarker{}, fmt.Errorf("prepare file-set evidence publication: %w", err)
	}
	if err := storagecommit.CommitPreparedTree(ctx, request); err != nil {
		if commitMayBeVisible(err) {
			prepared = true
		}
		return transactionMarker{}, fmt.Errorf("publish file-set transaction evidence: %w", err)
	}

	prepared = true
	return marker, nil
}

func loadMarker(ctx context.Context, path string) (transactionMarker, error) {
	canonicalPath, err := mutation.CanonicalDirectoryEntryPath(path)
	if err != nil {
		return transactionMarker{}, fmt.Errorf("canonicalize file-set transaction marker path: %w", err)
	}
	snapshot, err := storagecommit.ReadRegularFileSnapshotUpTo(ctx, canonicalPath, maximumMarkerBytes)
	if err != nil {
		return transactionMarker{}, err
	}
	if mode := snapshot.Mode().Perm(); mode != transactionEvidenceMode {
		return transactionMarker{}, fmt.Errorf(
			"file-set transaction marker %q permissions are %04o, want %04o",
			canonicalPath,
			mode,
			transactionEvidenceMode,
		)
	}
	content := snapshot.Content()
	if err := jsonstrict.Validate(content, "file-set transaction marker", maximumMarkerJSONDepth); err != nil {
		return transactionMarker{}, fmt.Errorf("parse file-set transaction marker %q: %w", canonicalPath, err)
	}
	var marker transactionMarker
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&marker); err != nil {
		return transactionMarker{}, fmt.Errorf("parse file-set transaction marker %q: %w", canonicalPath, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err == nil {
		return transactionMarker{}, fmt.Errorf("file-set transaction marker %q contains multiple JSON values", canonicalPath)
	} else if err != io.EOF {
		return transactionMarker{}, fmt.Errorf("parse file-set transaction marker %q: %w", canonicalPath, err)
	}
	if err := validateMarker(canonicalPath, marker); err != nil {
		return transactionMarker{}, err
	}
	return marker, nil
}

func validateMarker(path string, marker transactionMarker) error {
	if marker.Version != 1 && marker.Version != transactionVersion {
		return fmt.Errorf("unsupported file-set transaction marker version %d", marker.Version)
	}
	if len(marker.Targets) == 0 {
		return fmt.Errorf("file-set transaction marker requires targets")
	}
	markerDir := filepath.Dir(path)
	previousOrdinary := ""
	commitPointSeen := false
	seenPaths := make(map[string]struct{}, len(marker.Targets))
	for index, target := range marker.Targets {
		if target.Path == "" || !filepath.IsAbs(target.Path) || filepath.Clean(target.Path) != target.Path {
			return fmt.Errorf("file-set transaction marker target[%d] has invalid path %q", index, target.Path)
		}
		if _, exists := seenPaths[target.Path]; exists {
			return fmt.Errorf("file-set transaction marker targets must be unique and canonically ordered")
		}
		seenPaths[target.Path] = struct{}{}
		if marker.Version == 1 && target.CommitPoint {
			return fmt.Errorf("file-set transaction marker version 1 does not support a commit point")
		}
		if target.CommitPoint {
			if commitPointSeen {
				return fmt.Errorf("file-set transaction marker permits at most one commit point")
			}
			if !target.Write {
				return fmt.Errorf("file-set transaction marker commit point must write an after-image")
			}
			commitPointSeen = true
		} else {
			if commitPointSeen ||
				(previousOrdinary != "" && target.Path <= previousOrdinary) {
				return fmt.Errorf("file-set transaction marker targets must be unique and canonically ordered")
			}
			previousOrdinary = target.Path
		}
		if target.Write {
			if !isHash(target.AfterHash) {
				return fmt.Errorf("file-set transaction marker target[%d] has invalid after hash", index)
			}
		} else if target.AfterHash != "" {
			return fmt.Errorf("file-set transaction marker retained target[%d] must not have an after hash", index)
		}
		expectedBackup := filepath.Join(markerDir, fmt.Sprintf("target-%03d.before", index))
		if err := validateBeforeState(fmt.Sprintf("targets[%d].before", index), target.Before, expectedBackup); err != nil {
			return err
		}
	}
	return nil
}

func validateBeforeState(name string, state fileState, expectedBackupPath string) error {
	if !state.Exists {
		if state.Hash != "" || state.BackupPath != "" || state.Mode != 0 {
			return fmt.Errorf("%s absent state must not retain file metadata", name)
		}
		return nil
	}
	if !isHash(state.Hash) {
		return fmt.Errorf("%s has invalid content hash", name)
	}
	if state.BackupPath != expectedBackupPath {
		return fmt.Errorf("%s backup path %q does not match transaction evidence", name, state.BackupPath)
	}
	if state.Mode == 0 || os.FileMode(state.Mode).Perm() != os.FileMode(state.Mode) {
		return fmt.Errorf("%s has invalid file mode", name)
	}
	return nil
}

func captureFileState(ctx context.Context, path string, stagedBackupPath string, activeBackupPath string) (fileState, error) {
	content, mode, err := storagecommit.ReadRegularFile(ctx, path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fileState{}, nil
		}
		return fileState{}, fmt.Errorf("capture before-image %q: %w", path, err)
	}
	if err := os.WriteFile(stagedBackupPath, content, transactionEvidenceMode); err != nil {
		return fileState{}, fmt.Errorf("stage before-image %q: %w", path, err)
	}
	return fileState{
		Exists:     true,
		Hash:       hashBytes(content),
		BackupPath: activeBackupPath,
		Mode:       uint32(mode.Perm()),
	}, nil
}

func canonicalStateDir(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("file-set transaction state dir is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve file-set transaction state dir %q: %w", path, err)
	}
	probe, err := mutation.CanonicalDirectoryEntryPath(filepath.Join(filepath.Clean(absolute), ".metadata-state-probe"))
	if err != nil {
		return "", fmt.Errorf("canonicalize file-set transaction state dir %q: %w", path, err)
	}
	return filepath.Dir(probe), nil
}

func transactionDir(stateDir string) string {
	return filepath.Join(stateDir, transactionDirName)
}

func markerPath(stateDir string) string {
	return filepath.Join(transactionDir(stateDir), transactionMarkerFile)
}

func hashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func isHash(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil && len(decoded) == sha256.Size
}
