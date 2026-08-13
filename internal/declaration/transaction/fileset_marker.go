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

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/mutation"
	storagecommit "github.com/isty2e/daem/internal/effect/storage/commit"
	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const (
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
	if err := storagecommit.PrepareCommitParent(ctx, transactionDir); err != nil {
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
		Version: contractversion.MetadataTransaction,
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

	content, err := marshalMarker(marker)
	if err != nil {
		return transactionMarker{}, fmt.Errorf("marshal file-set transaction marker: %w", err)
	}
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

func marshalMarker(marker transactionMarker) ([]byte, error) {
	content, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return nil, err
	}
	content = append(content, '\n')
	if int64(len(content)) > maximumMarkerBytes {
		return nil, fmt.Errorf(
			"file-set transaction marker contains %d bytes, maximum %d",
			len(content),
			maximumMarkerBytes,
		)
	}
	return content, nil
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
	envelope, err := jsonstrict.DecodeVersionEnvelope(
		content,
		"file-set transaction marker",
		maximumMarkerJSONDepth,
		contractversion.MetadataTransaction,
	)
	if err != nil {
		return transactionMarker{}, fmt.Errorf("parse file-set transaction marker %q: %w", canonicalPath, err)
	}
	switch envelope.Disposition {
	case jsonstrict.VersionLegacy:
		return transactionMarker{}, fmt.Errorf(
			"unsupported legacy file-set transaction marker version %d; use the daem version that wrote it to recover before upgrading and do not delete the transaction evidence",
			envelope.Version,
		)
	case jsonstrict.VersionFuture:
		return transactionMarker{}, fmt.Errorf(
			"unsupported file-set transaction marker version %d; it was written by a newer daem, so upgrade daem before recovery and do not delete the transaction evidence",
			envelope.Version,
		)
	}
	if err := validateMarkerPresence(content); err != nil {
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

func validateMarkerPresence(content []byte) error {
	// Inspect raw fields before typed decoding can turn omitted or null
	// authority into zero-value before-image evidence.
	var root map[string]json.RawMessage
	if err := json.Unmarshal(content, &root); err != nil {
		return fmt.Errorf("decode marker fields: %w", err)
	}
	if _, err := requireMarkerField(root, "version", "file-set transaction marker"); err != nil {
		return err
	}
	targets, err := requireMarkerField(root, "targets", "file-set transaction marker")
	if err != nil {
		return err
	}
	var targetValues []json.RawMessage
	if err := json.Unmarshal(targets, &targetValues); err != nil {
		return fmt.Errorf("file-set transaction marker field %q must be an array: %w", "targets", err)
	}
	for index, targetValue := range targetValues {
		var target map[string]json.RawMessage
		if err := json.Unmarshal(targetValue, &target); err != nil {
			return fmt.Errorf("targets[%d] must be an object: %w", index, err)
		}
		prefix := fmt.Sprintf("targets[%d]", index)
		if _, err := requireMarkerField(target, "path", prefix); err != nil {
			return err
		}
		if err := rejectMarkerNull(target, "after_hash", prefix); err != nil {
			return err
		}
		if err := rejectMarkerNull(target, "commit_point", prefix); err != nil {
			return err
		}
		before, err := requireMarkerField(target, "before", prefix)
		if err != nil {
			return err
		}
		write, err := requireMarkerField(target, "write", prefix)
		if err != nil {
			return err
		}
		var writeValue bool
		if err := json.Unmarshal(write, &writeValue); err != nil {
			return fmt.Errorf("%s field %q must be a boolean: %w", prefix, "write", err)
		}
		if writeValue {
			if _, err := requireMarkerField(target, "after_hash", prefix); err != nil {
				return err
			}
		}
		if err := validateBeforePresence(before, prefix+".before"); err != nil {
			return err
		}
	}
	return nil
}

func validateBeforePresence(content json.RawMessage, name string) error {
	var state map[string]json.RawMessage
	if err := json.Unmarshal(content, &state); err != nil {
		return fmt.Errorf("%s must be an object: %w", name, err)
	}
	exists, err := requireMarkerField(state, "exists", name)
	if err != nil {
		return err
	}
	var existsValue bool
	if err := json.Unmarshal(exists, &existsValue); err != nil {
		return fmt.Errorf("%s field %q must be a boolean: %w", name, "exists", err)
	}
	for _, field := range []string{"hash", "backup_path", "mode"} {
		if err := rejectMarkerNull(state, field, name); err != nil {
			return err
		}
	}
	if !existsValue {
		return nil
	}
	for _, field := range []string{"hash", "backup_path", "mode"} {
		if _, err := requireMarkerField(state, field, name); err != nil {
			return err
		}
	}
	return nil
}

func requireMarkerField(fields map[string]json.RawMessage, field string, object string) (json.RawMessage, error) {
	value, ok := fields[field]
	if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return nil, fmt.Errorf("%s field %q is required and must not be null", object, field)
	}
	return value, nil
}

func rejectMarkerNull(fields map[string]json.RawMessage, field string, object string) error {
	value, ok := fields[field]
	if ok && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return fmt.Errorf("%s field %q must not be null", object, field)
	}
	return nil
}

func validateMarker(path string, marker transactionMarker) error {
	if marker.Version != contractversion.MetadataTransaction {
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
	content, mode, err := readTransactionFile(ctx, path)
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
