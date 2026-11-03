package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
	daempaths "github.com/isty2e/daem/internal/paths"
	workflowadopt "github.com/isty2e/daem/internal/workflow/adopt"
)

// The persisted generation-4 shape is the recovery input under test.
// Private writer fault injection is covered by the file-set owner's tests.
func interruptedCapture(t *testing.T, manifest, source string, before, after, sourceBefore, sourceAfter []byte, finalized bool) string {
	t.Helper()
	canonical := func(path string) string {
		result, err := mutation.CanonicalDirectoryEntryPath(path)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	manifest, source = canonical(manifest), canonical(source)
	evidence, err := fileset.FileSetAuthorityPath(filepath.Join(filepath.Dir(manifest), ".daem"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(path string, content []byte) {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash := func(content []byte) string { return fmt.Sprintf("sha256:%x", sha256.Sum256(content)) }
	type beforeImage struct {
		Exists     bool   `json:"exists"`
		Hash       string `json:"hash"`
		BackupPath string `json:"backup_path"`
		Mode       uint32 `json:"mode"`
	}
	type target struct {
		Path        string      `json:"path"`
		Before      beforeImage `json:"before"`
		AfterHash   string      `json:"after_hash"`
		Write       bool        `json:"write"`
		CommitPoint bool        `json:"commit_point,omitempty"`
	}
	var rows []target
	if sourceAfter != nil {
		prior := beforeImage{}
		if sourceBefore != nil {
			backup := filepath.Join(evidence, "target-000.before")
			write(backup, sourceBefore)
			prior = beforeImage{true, hash(sourceBefore), backup, 0o600}
		}
		rows = append(rows, target{Path: source, Before: prior, AfterHash: hash(sourceAfter), Write: true})
		write(source, sourceAfter)
	}
	prior := beforeImage{}
	if before != nil {
		backup := filepath.Join(evidence, fmt.Sprintf("target-%03d.before", len(rows)))
		write(backup, before)
		prior = beforeImage{true, hash(before), backup, 0o600}
		write(manifest, before)
	} else if err := os.Remove(manifest); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	rows = append(rows, target{Path: manifest, Before: prior, AfterHash: hash(after), Write: true, CommitPoint: true})
	if finalized {
		write(manifest, after)
	}
	write(filepath.Join(evidence, "reference.after"), after)
	marker := struct {
		Version   int      `json:"version"`
		Targets   []target `json:"targets"`
		Reference struct {
			Target     string `json:"target"`
			AfterImage struct {
				Path   string `json:"path"`
				Size   int64  `json:"size"`
				Digest string `json:"digest"`
			} `json:"after_image"`
		} `json:"reference"`
	}{Version: contractversion.MetadataTransactionReference, Targets: rows}
	marker.Reference.Target = manifest
	marker.Reference.AfterImage.Path, marker.Reference.AfterImage.Size, marker.Reference.AfterImage.Digest = "reference.after", int64(len(after)), hash(after)
	encoded, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(evidence, "transaction.json")
	write(path, encoded)
	return path
}

func TestImportHostConfigRecoversInitialPublicationWithoutRecapture(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		t.Run(fmt.Sprint(finalized), func(t *testing.T) {
			_, manifest, host, args := captureCLIFixture(t, `{"chosen":1}`)
			args = append(args, "--key", "/chosen")
			fingerprint := captureFingerprint(t, args)
			if code, out, err := runCaptureCLI(t, append(append([]string{}, args...), "--yes", "--expect-plan", fingerprint)); code != 0 {
				t.Fatal(code, out, err)
			}
			after, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(filepath.Dir(manifest), "daem.d", "host-config", "chosen.json")
			sourceAfter, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			marker := interruptedCapture(t, manifest, source, nil, after, nil, sourceAfter, finalized)
			if err := os.Remove(host); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(host, 0o700); err != nil {
				t.Fatal(err)
			}
			beforeMarker, err := os.ReadFile(marker)
			if err != nil {
				t.Fatal(err)
			}
			fingerprint = captureFingerprint(t, args)
			unchanged, err := os.ReadFile(marker)
			if err != nil || !bytes.Equal(beforeMarker, unchanged) {
				t.Fatal("recovery preview mutated evidence", err)
			}
			code, stdout, stderr := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint, "--json"))
			if code != 0 || !strings.Contains(stdout, `"recovery"`) || !strings.Contains(stdout, `"recovered"`) {
				t.Fatal("recovery attempted fresh host read/capture", code, stdout, stderr)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("recovery retained marker", err)
			}
			if finalized {
				current, err := os.ReadFile(source)
				if err != nil || !bytes.Equal(current, sourceAfter) {
					t.Fatal("finalize changed captured source", err)
				}
			} else {
				for _, path := range []string{manifest, source} {
					if _, err := os.Stat(path); !os.IsNotExist(err) {
						t.Fatal("rollback did not remove initial artifact", path, err)
					}
				}
			}
		})
	}
}

func TestImportHostConfigRecoveryRejectsWrongDeclarationAndChangedApproval(t *testing.T) {
	_, manifest, _, args := captureCLIFixture(t, `{"chosen":1}`)
	args = append(args, "--key", "/chosen")
	fingerprint := captureFingerprint(t, args)
	if code, out, err := runCaptureCLI(t, append(append([]string{}, args...), "--yes", "--expect-plan", fingerprint)); code != 0 {
		t.Fatal(code, out, err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(filepath.Dir(manifest), "daem.d", "host-config", "chosen.json")
	content, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	marker := interruptedCapture(t, manifest, source, nil, after, nil, content, false)
	wrong := append([]string{}, args...)
	wrong[2] = "other"
	if code, _, _ := runCaptureCLI(t, append(wrong, "--dry-run")); code != 1 {
		t.Fatal("recovered wrong declaration", code)
	}
	fingerprint = captureFingerprint(t, args)
	encoded, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, append(encoded, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint)); code != 1 {
		t.Fatal("changed marker retained approval", code)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("refused recovery consumed marker", err)
	}
}

func TestUninitializedCaptureRecoveryCannotExecute(t *testing.T) {
	if _, err := workflowadopt.ExecuteHostConfigCaptureRecovery(t.Context(), nil, ""); err == nil {
		t.Fatal("nil recovery executed")
	}
}

func TestImportHostConfigRecoversSourceSwitchAndRetirement(t *testing.T) {
	for _, retirement := range []bool{false, true} {
		for _, finalized := range []bool{false, true} {
			t.Run(fmt.Sprintf("retire=%t/finalized=%t", retirement, finalized), func(t *testing.T) {
				_, manifest, host, args := captureCLIFixture(t, `{"chosen":1}`)
				if retirement {
					whole := filepath.Join(filepath.Dir(host), "whole.json")
					if err := os.Rename(host, whole); err != nil {
						t.Fatal(err)
					}
					host = whole
					for index, argument := range args {
						if argument == "--host-path" {
							args[index+1] = "whole.json"
						}
					}
					args = append(args, "--ownership", "document")
				} else {
					args = append(args, "--key", "/chosen")
				}
				fingerprint := captureFingerprint(t, args)
				if code, out, err := runCaptureCLI(t, append(args, "--yes", "--expect-plan", fingerprint)); code != 0 {
					t.Fatal(code, out, err)
				}
				originalSource := filepath.Join(filepath.Dir(manifest), "daem.d", "host-config", "chosen.json")
				originalBytes, err := os.ReadFile(originalSource)
				if err != nil {
					t.Fatal(err)
				}
				if retirement {
					if err := os.Remove(host); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(host, []byte(`{"chosen":2}`), 0o600); err != nil {
					t.Fatal(err)
				}
				paths, err := daempaths.Resolve(manifest)
				if err != nil {
					t.Fatal(err)
				}
				export := ""
				if !retirement {
					export = filepath.Join(filepath.Dir(manifest), "isolated.json")
				}
				plan, err := workflowadopt.BuildHostConfigUpdatePlan(t.Context(), workflowadopt.HostConfigUpdateInput{Paths: paths, Name: "chosen", SourcePath: export})
				if err != nil {
					t.Fatal(err)
				}
				sourceAfter := plan.SourceAfter()
				if retirement {
					sourceAfter = nil
				}
				marker := interruptedCapture(t, manifest, plan.SourcePath(), plan.ManifestBefore(), plan.ManifestAfter(), nil, sourceAfter, finalized)
				if err := plan.Close(); err != nil {
					t.Fatal(err)
				}
				update := []string{"import", "host-config", "chosen", "--manifest", manifest, "--update-source"}
				if !retirement {
					update = append(update, "--source", export)
				}
				fingerprint = captureFingerprint(t, update)
				if code, out, err := runCaptureCLI(t, append(update, "--yes", "--expect-plan", fingerprint)); code != 0 {
					t.Fatal("recovery failed", code, out, err)
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatal("recovery did not terminate", err)
				}
				unchanged, err := os.ReadFile(originalSource)
				if err != nil || !bytes.Equal(unchanged, originalBytes) {
					t.Fatal("recovery changed old source", err)
				}
				if !retirement {
					if finalized {
						if actual, err := os.ReadFile(export); err != nil || !bytes.Equal(actual, sourceAfter) {
							t.Fatal("finalize lost exported source", err)
						}
					} else if _, err := os.Stat(export); !os.IsNotExist(err) {
						t.Fatal("rollback retained export", err)
					}
				}
			})
		}
	}
}
