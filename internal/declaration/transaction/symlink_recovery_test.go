package transaction

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
)

func TestRecoveryBindsRecordedDeclarationReferents(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	dot := filepath.Join(root, "dot")
	for _, dir := range []string{project, dot} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(project, "daem.toml")
	lockfile := filepath.Join(project, "daem.lock.toml")
	writeAdapterFixture(t, filepath.Join(dot, "a-manifest"), "manifest-before", 0o600)
	writeAdapterFixture(t, filepath.Join(dot, "z-lock"), "lock-before", 0o600)
	if err := os.Symlink("../dot/a-manifest", manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dot/z-lock", lockfile); err != nil {
		t.Fatal(err)
	}
	manifestTarget := captureAdapterTarget(t, manifest)
	lockTarget := captureAdapterTarget(t, lockfile)
	stateDir := filepath.Join(project, ".daem")
	evidence, err := fileset.FileSetAuthorityPath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	hash := func(value string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value))) }
	type beforeImage struct {
		Exists     bool   `json:"exists"`
		Hash       string `json:"hash"`
		BackupPath string `json:"backup_path"`
		Mode       uint32 `json:"mode"`
	}
	type recordedTarget struct {
		Path      string      `json:"path"`
		Write     bool        `json:"write"`
		AfterHash string      `json:"after_hash"`
		Before    beforeImage `json:"before"`
	}
	rows := make([]recordedTarget, 0, 2)
	for index, file := range []struct{ path, before, after string }{
		{manifestTarget.Path(), "manifest-before", "manifest-after"},
		{lockTarget.Path(), "lock-before", "lock-after"},
	} {
		backup := filepath.Join(evidence, fmt.Sprintf("target-%03d.before", index))
		writeAdapterFixture(t, backup, file.before, 0o600)
		rows = append(rows, recordedTarget{
			Path: file.path, Write: true, AfterHash: hash(file.after),
			Before: beforeImage{Exists: true, Hash: hash(file.before), BackupPath: backup, Mode: 0o600},
		})
	}
	content, err := json.Marshal(struct {
		Version int              `json:"version"`
		Targets []recordedTarget `json:"targets"`
	}{Version: contractversion.MetadataTransaction, Targets: rows})
	if err != nil {
		t.Fatal(err)
	}
	writeAdapterFixture(t, filepath.Join(evidence, "transaction.json"), string(content), 0o600)
	writeAdapterFixture(t, manifestTarget.Path(), "manifest-after", 0o600)
	writeAdapterFixture(t, filepath.Join(dot, "other-lock"), "unrelated", 0o600)
	if err := os.Remove(lockfile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dot/other-lock", lockfile); err != nil {
		t.Fatal(err)
	}

	err = RecoverInterruptedTransaction(context.Background(), stateDir, manifest, lockfile)
	var authority fileset.RecoveryAuthorityError
	if !errors.As(err, &authority) || authority.Path != lockTarget.Path() {
		t.Fatalf("recovery error = %v, want recorded lock target refusal", err)
	}
	assertAdapterContent(t, manifestTarget.Path(), "manifest-after")
	assertAdapterContent(t, filepath.Join(dot, "other-lock"), "unrelated")
	if _, err := os.Stat(evidence); err != nil {
		t.Fatalf("refused recovery discarded evidence: %v", err)
	}
	if err := os.Remove(lockfile); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../dot/z-lock", lockfile); err != nil {
		t.Fatal(err)
	}
	if err := RecoverInterruptedTransaction(context.Background(), stateDir, manifest, lockfile); err != nil {
		t.Fatal(err)
	}
	assertAdapterContent(t, manifest, "manifest-before")
	assertAdapterContent(t, lockfile, "lock-before")
	if _, err := os.Lstat(evidence); !os.IsNotExist(err) {
		t.Fatalf("successful recovery retained marker: %v", err)
	}
	if link, err := os.Readlink(lockfile); err != nil || link != "../dot/z-lock" {
		t.Fatalf("recovery changed selected link: %q, %v", link, err)
	}
}
