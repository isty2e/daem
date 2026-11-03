package transaction

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/contractversion"
	"github.com/isty2e/daem/internal/effect/fileset"
	"github.com/isty2e/daem/internal/effect/mutation"
)

type (
	sourceRecoveryFixture struct{ stateDir, source, manifest, marker string }
	sourceRecoverySetup   struct{ retirement, finalized, initial bool }
)

func TestUninitializedSourceRecoveryCannotMutate(t *testing.T) {
	if disposition, err := (SourceRecovery{}).Recover(t.Context()); err == nil || disposition != "" {
		t.Fatal("uninitialized recovery was admitted", disposition, err)
	}
}

func newSourceRecoveryFixture(t *testing.T, setup sourceRecoverySetup) sourceRecoveryFixture {
	t.Helper()
	root := t.TempDir()
	fixture := sourceRecoveryFixture{stateDir: filepath.Join(root, ".daem"), source: filepath.Join(root, "source"), manifest: filepath.Join(root, "manifest")}
	writeAdapterFixture(t, fixture.source, "source-after", 0o600)
	if !setup.initial {
		writeAdapterFixture(t, fixture.manifest, "manifest-before", 0o600)
	}
	fixture.source = captureAdapterTarget(t, fixture.source).Path()
	fixture.manifest = captureAdapterTarget(t, fixture.manifest).Path()
	if setup.finalized {
		writeAdapterFixture(t, fixture.manifest, "manifest-after", 0o600)
	}
	evidence, err := fileset.FileSetAuthorityPath(fixture.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
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
	hash := func(content string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(content))) }
	var rows []target
	if !setup.retirement {
		backup := filepath.Join(evidence, "target-000.before")
		before := beforeImage{}
		if !setup.initial {
			writeAdapterFixture(t, backup, "source-before", 0o600)
			before = beforeImage{true, hash("source-before"), backup, 0o600}
		}
		rows = append(rows, target{Path: fixture.source, Before: before, AfterHash: hash("source-after"), Write: true})
	}
	backup := filepath.Join(evidence, fmt.Sprintf("target-%03d.before", len(rows)))
	before := beforeImage{}
	if !setup.initial {
		writeAdapterFixture(t, backup, "manifest-before", 0o600)
		before = beforeImage{true, hash("manifest-before"), backup, 0o600}
	}
	rows = append(rows, target{Path: fixture.manifest, Before: before, AfterHash: hash("manifest-after"), Write: true, CommitPoint: true})
	writeAdapterFixture(t, filepath.Join(evidence, "reference.after"), "manifest-after", 0o600)
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
	marker.Reference.Target = fixture.manifest
	marker.Reference.AfterImage.Path, marker.Reference.AfterImage.Size, marker.Reference.AfterImage.Digest = "reference.after", int64(len("manifest-after")), hash("manifest-after")
	content, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	fixture.marker = filepath.Join(evidence, "transaction.json")
	writeAdapterFixture(t, fixture.marker, string(content), 0o600)
	return fixture
}

func TestSourceRecoveryPreviewIsReadOnlyAndRecoveryNeverRecaptures(t *testing.T) {
	for _, retirement := range []bool{false, true} {
		for _, finalized := range []bool{false, true} {
			t.Run(fmt.Sprintf("retirement=%t/finalized=%t", retirement, finalized), func(t *testing.T) {
				fixture := newSourceRecoveryFixture(t, sourceRecoverySetup{retirement: retirement, finalized: finalized})
				manifest, source := captureAdapterTarget(t, fixture.manifest), captureAdapterTarget(t, fixture.source)
				markerBefore, err := os.ReadFile(fixture.marker)
				if err != nil {
					t.Fatal(err)
				}
				var preview SourceRecovery
				if retirement {
					preview, err = InspectSourceRetirementRecovery(t.Context(), fixture.stateDir, manifest)
				} else {
					preview, err = InspectSourceTransactionRecovery(t.Context(), fixture.stateDir, manifest, source)
				}
				if err != nil {
					t.Fatal(err)
				}
				want := fileset.RecoveryRollback
				if finalized {
					want = fileset.RecoveryFinalize
				}
				if preview.Disposition() != want {
					t.Fatal("incorrect recovery decision", preview.Disposition())
				}
				before, existed := preview.Reference().BeforeImage()
				if !existed || string(before) != "manifest-before" || string(preview.Reference().AfterImage()) != "manifest-after" {
					t.Fatal("preview lost declaration evidence")
				}
				markerAfter, err := os.ReadFile(fixture.marker)
				if err != nil || !bytes.Equal(markerBefore, markerAfter) {
					t.Fatal("preview changed evidence", err)
				}
				assertAdapterContent(t, fixture.source, "source-after")
				manifestContent := "manifest-before"
				if finalized {
					manifestContent = "manifest-after"
				}
				assertAdapterContent(t, fixture.manifest, manifestContent)
				result, err := preview.Recover(t.Context())
				if err != nil || result != want {
					t.Fatal("recovery failed", result, err)
				}
				if !retirement && !finalized {
					assertAdapterContent(t, fixture.source, "source-before")
				} else {
					assertAdapterContent(t, fixture.source, "source-after")
				}
				assertAdapterContent(t, fixture.manifest, manifestContent)
				if _, err := os.Stat(fixture.marker); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("recovery left/recreated the marker", err)
				}
				if _, err := preview.Recover(t.Context()); err == nil {
					t.Fatal("consumed recovery was replayed")
				}
			})
		}
	}
}

func TestSourceRecoveryRefusesWrongScopeAndChangedApprovalEvidence(t *testing.T) {
	for _, change := range []string{"wrong-source", "missing-source", "extra-source", "wrong-reference", "marker", "source"} {
		t.Run(change, func(t *testing.T) {
			fixture := newSourceRecoveryFixture(t, sourceRecoverySetup{retirement: change == "extra-source"})
			manifest, source := captureAdapterTarget(t, fixture.manifest), captureAdapterTarget(t, fixture.source)
			if change == "wrong-source" {
				other := filepath.Join(filepath.Dir(fixture.source), "other")
				writeAdapterFixture(t, other, "unrelated", 0o600)
				source = captureAdapterTarget(t, other)
			}
			if change == "wrong-reference" {
				manifest, source = source, manifest
			}
			var preview SourceRecovery
			var err error
			if change == "missing-source" {
				preview, err = InspectSourceRetirementRecovery(t.Context(), fixture.stateDir, manifest)
			} else {
				preview, err = InspectSourceTransactionRecovery(t.Context(), fixture.stateDir, manifest, source)
			}
			if change != "marker" && change != "source" {
				if err == nil {
					t.Fatal("recovery admitted marker-owned target authority")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if change == "marker" {
					content, err := os.ReadFile(fixture.marker)
					if err != nil {
						t.Fatal(err)
					}
					writeAdapterFixture(t, fixture.marker, " "+string(content), 0o600)
				} else {
					writeAdapterFixture(t, fixture.source, "external-edit", 0o600)
				}
				_, err = preview.Recover(t.Context())
				var stale mutation.StaleSnapshotError
				if !errors.As(err, &stale) {
					t.Fatal("changed approval evidence was not refused", err)
				}
			}
			assertAdapterContent(t, fixture.manifest, "manifest-before")
			wantSource := "source-after"
			if change == "source" {
				wantSource = "external-edit"
			}
			assertAdapterContent(t, fixture.source, wantSource)
			if _, err := os.Stat(fixture.marker); err != nil {
				t.Fatal("refusal discarded evidence", err)
			}
		})
	}
}

func TestInitialSourceRecoveryHandlesAbsentBeforeImages(t *testing.T) {
	for _, finalized := range []bool{false, true} {
		fixture := newSourceRecoveryFixture(t, sourceRecoverySetup{initial: true, finalized: finalized})
		preview, err := InspectSourceTransactionRecovery(t.Context(), fixture.stateDir, captureAdapterTarget(t, fixture.manifest), captureAdapterTarget(t, fixture.source))
		if err != nil {
			t.Fatal(err)
		}
		if _, existed := preview.Reference().BeforeImage(); existed {
			t.Fatal("initial recovery manufactured a prior declaration")
		}
		if _, err := preview.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		if finalized {
			assertAdapterContent(t, fixture.manifest, "manifest-after")
			assertAdapterContent(t, fixture.source, "source-after")
		} else {
			for _, path := range []string{fixture.manifest, fixture.source} {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("initial rollback retained a newly created artifact", path, err)
				}
			}
		}
	}
}
