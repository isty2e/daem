package metadatatx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

type Image struct {
	Content []byte
	Mode    os.FileMode
}

type Write struct {
	Path        string
	Before      *Image
	After       []byte
	CommitPoint bool
}

// WritePrefix installs persisted before/after evidence and a selected prefix of
// visible writes. It models interruption state; it does not simulate power loss.
func WritePrefix(t testing.TB, stateDir string, writes []Write, visible int) {
	t.Helper()
	if visible < 0 || visible > len(writes) {
		t.Fatal("invalid visible prefix")
	}
	root, err := fileset.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writes = append([]Write(nil), writes...)
	for index := range writes {
		writes[index].Path, err = mutation.CanonicalDirectoryEntryPath(writes[index].Path)
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].CommitPoint != writes[j].CommitPoint {
			return !writes[i].CommitPoint
		}
		return writes[i].Path < writes[j].Path
	})
	type beforeState struct {
		Exists     bool   `json:"exists"`
		Hash       string `json:"hash,omitempty"`
		BackupPath string `json:"backup_path,omitempty"`
		Mode       uint32 `json:"mode,omitempty"`
	}
	type row struct {
		Path        string      `json:"path"`
		Before      beforeState `json:"before"`
		AfterHash   string      `json:"after_hash"`
		Write       bool        `json:"write"`
		CommitPoint bool        `json:"commit_point,omitempty"`
	}
	marker := struct {
		Version int   `json:"version"`
		Targets []row `json:"targets"`
	}{Version: contractversion.MetadataTransaction}
	for index, write := range writes {
		entry := row{Path: write.Path, AfterHash: contentHash(write.After), Write: true, CommitPoint: write.CommitPoint}
		mode := os.FileMode(0o600)
		if write.Before != nil {
			backup := filepath.Join(root, fmt.Sprintf("target-%03d.before", index))
			entry.Before = beforeState{Exists: true, Hash: contentHash(write.Before.Content), BackupPath: backup, Mode: uint32(write.Before.Mode)}
			if err := os.WriteFile(backup, write.Before.Content, 0o600); err != nil {
				t.Fatal(err)
			}
			mode = write.Before.Mode
		}
		marker.Targets = append(marker.Targets, entry)
		if index >= visible && write.Before == nil {
			if err := os.Remove(write.Path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			continue
		}
		content := write.After
		if index >= visible {
			content = write.Before.Content
		}
		if err := os.MkdirAll(filepath.Dir(write.Path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(write.Path, content, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(write.Path, mode); err != nil {
			t.Fatal(err)
		}
	}
	content, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "transaction.json"), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func contentHash(content []byte) string {
	hash := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(hash[:])
}
