package journal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestNativeInventoryRetainsActualRecordAndFeedsDocumentAssessment(t *testing.T) {
	setup, root, path := nativeInventoryFixture(t)
	_, read := setup.reader(t)
	options := inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec, PhysicalPathBudget: read.documents.budget}
	inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, options)
	if err != nil || inventory.decision.State() != retirement.StateActive || inventory.active == nil {
		t.Fatalf("native active inventory: %v, %s", err, inventory.decision.Detail())
	}
	current, native := inventory.active.record.(nativeJournalSnapshots)
	if !native || !current.before.Equal(setup.fixture.before) || !current.after.Equal(setup.fixture.after) {
		t.Fatal("native inventory lost its complete typed Snapshots")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.active.identity.JournalAuthorityFingerprint() != recoveryJournalRecordFingerprint(content) {
		t.Fatal("native inventory fingerprint differs from canonical full journal bytes")
	}
	if err := inventory.active.physicalAuthority.Validate(); err != nil {
		t.Fatal(err)
	}
	assessment, err := current.assessDocumentRecovery(t.Context(), read)
	if err != nil || assessment.disposition != nativeDocumentsBefore || len(assessment.stages) != 1 {
		t.Fatalf("native actual inventory-to-state/registry/image/host pipeline: %v", err)
	}
	if err := setup.fixture.read.artifacts.ValidateSelectionBounded(inventory.active.operationDir, recovery.MaximumPhysicalPathDepth, read.documents.budget); err != nil {
		t.Fatalf("test image grant is not the selected active directory: %v", err)
	}
	retained, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(retained, content) {
		t.Fatalf("read-only native inventory changed its journal: %v", err)
	}
	called := false
	legacy, err := loadActivePlanFromInventory(t.Context(), Paths{RecoveryDir: root}, nil, nil, PlanLoadOptions{
		ValidateBeforeActiveObservation: func(context.Context) error { called = true; return nil },
	}, inventory)
	if err == nil || legacy.Classification() != "" || called {
		t.Fatal("native record reached the legacy plan or its pre-observation callback")
	}
}

func TestDefaultInventoryRejectsNativeBeforeStateDecoding(t *testing.T) {
	setup, root, _ := nativeInventoryFixture(t)
	codec := &nativeInventoryCodecProbe{SnapshotCodec: setup.fixture.codec}
	inventory, err := loadRecoveryRootInventory(t.Context(), root, inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: codec})
	if err != nil || inventory.decision.State() != retirement.StateBlocked || inventory.active != nil || codec.decodes != 0 {
		t.Fatalf("default inventory admitted native state or read it before refusal: %v, decodes=%d", err, codec.decodes)
	}
}

func TestNativeAndDefaultInventoryKeepGenerationIsolation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "recovery")
	identity, _ := captureInventoryJournal(t, root, "ordinary-native-reader")
	inventory, err := loadRecoveryRootInventory(t.Context(), root, inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: testStateCodec()})
	if err != nil || inventory.active == nil || inventory.decision.State() != retirement.StateActive {
		t.Fatalf("default reader lost ordinary v15 recovery: %v", err)
	}
	legacy, ok := inventory.active.record.(recoveryJournal)
	if !ok || legacy.OperationID != identity.OperationID() || inventory.active.identity.JournalAuthorityFingerprint() != identity.JournalAuthorityFingerprint() {
		t.Fatal("default reader changed legacy record/fingerprint semantics")
	}
	native, err := statefile.NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	probe := &nativeInventoryCodecProbe{SnapshotCodec: native}
	rejected, err := loadNativeRecoveryRootInventory(t.Context(), root, inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: probe})
	if err != nil || rejected.decision.State() != retirement.StateBlocked || rejected.active != nil || probe.decodes != 0 {
		t.Fatalf("native reader admitted pending v15 or decoded its state: %v, decodes=%d", err, probe.decodes)
	}
}

func TestNativeInventoryCorrelatesCompleteRecordWithControlAndResidue(t *testing.T) {
	for _, state := range []retirement.State{retirement.StatePrepared, retirement.StateRetained, retirement.StateFinalizing} {
		t.Run(string(state), func(t *testing.T) {
			setup, root, path := nativeInventoryFixture(t)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := retirement.NewIdentity(setup.fixture.record.OperationID, recoveryJournalRecordFingerprint(content))
			if err != nil {
				t.Fatal(err)
			}
			phase := retirement.PhasePrepared
			if state == retirement.StateFinalizing {
				phase = retirement.PhaseFinalizing
			}
			writeInventoryControl(t, root, identity, phase)
			if state != retirement.StatePrepared {
				residue := filepath.Join(root, identity.ResidueName())
				if err := os.Rename(filepath.Dir(path), residue); err != nil {
					t.Fatal(err)
				}
				if state == retirement.StateFinalizing {
					if err := os.Remove(filepath.Join(residue, recoveryJournalFileName)); err != nil {
						t.Fatal(err)
					}
				}
			}
			inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec})
			if err != nil || inventory.decision.State() != state || (inventory.active != nil) != (state == retirement.StatePrepared) {
				t.Fatalf("native control/residue state=%s want=%s: %v, %s", inventory.decision.State(), state, err, inventory.decision.Detail())
			}
		})
	}
}

func TestNativeInventoryFingerprintIncludesGroupImageEvidence(t *testing.T) {
	setup, root, path := nativeInventoryFixture(t)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := retirement.NewIdentity(setup.fixture.record.OperationID, recoveryJournalRecordFingerprint(content))
	if err != nil {
		t.Fatal(err)
	}
	writeInventoryControl(t, root, identity, retirement.PhasePrepared)
	setup.fixture.record.DocumentGroups[0].Original.Artifact.Digest = artifact.HashFileContent([]byte("different actual-image witness"))
	changed, err := marshalNativeJournalRecord(t.Context(), setup.fixture.record, setup.fixture.before, setup.fixture.after, setup.fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec})
	if err != nil || inventory.decision.State() != retirement.StateBlocked || inventory.active != nil {
		t.Fatalf("changed complete native witness was paired with an old control identity: %v", err)
	}
}

func TestNativeInventoryRejectsUnknownAndMalformedRecordsBeforeState(t *testing.T) {
	for _, version := range []int{0, 14, 17} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			setup, root, path := nativeInventoryFixture(t)
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var frame map[string]json.RawMessage
			if err := json.Unmarshal(content, &frame); err != nil {
				t.Fatal(err)
			}
			frame["version"] = json.RawMessage(fmt.Sprint(version))
			frame["statefile_before"] = json.RawMessage(`{"malformed":"unknown native version must not read me"}`)
			content, err = json.Marshal(frame)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			codec := &nativeInventoryCodecProbe{SnapshotCodec: setup.fixture.codec}
			inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: codec})
			if err != nil || inventory.decision.State() != retirement.StateBlocked || inventory.active != nil || codec.decodes != 0 {
				t.Fatalf("unknown version reached state decoding or yielded active evidence: %v, %d", err, codec.decodes)
			}
		})
	}
}

func TestNativeInventoryRejectsInconsistentActiveLayouts(t *testing.T) {
	for _, kind := range []string{"directory name", "mixed families", "multiple native records", "record revision"} {
		t.Run(kind, func(t *testing.T) {
			setup, root, path := nativeInventoryFixture(t)
			options := inventoryOptions{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec}
			switch kind {
			case "directory name":
				if err := os.Rename(filepath.Dir(path), filepath.Join(root, "foreign-operation")); err != nil {
					t.Fatal(err)
				}
			case "mixed families":
				_, ordinary := captureInventoryJournal(t, filepath.Join(t.TempDir(), "other-recovery"), "another-ordinary-operation")
				if err := os.Rename(ordinary.Directory, filepath.Join(root, "another-ordinary-operation")); err != nil {
					t.Fatal(err)
				}
			case "multiple native records":
				other, _, otherPath := nativeInventoryFixture(t)
				other.fixture.record.OperationID = "another-native-operation"
				content, err := marshalNativeJournalRecord(t.Context(), other.fixture.record, other.fixture.before, other.fixture.after, other.fixture.codec)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(otherPath, content, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(filepath.Dir(otherPath), filepath.Join(root, other.fixture.record.OperationID)); err != nil {
					t.Fatal(err)
				}
			case "record revision":
				content, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				probe := &nativeInventoryCodecProbe{SnapshotCodec: setup.fixture.codec}
				probe.beforeDecode = func() {
					probe.beforeDecode = nil
					replacement := filepath.Join(filepath.Dir(path), ".replacement")
					if err := os.WriteFile(replacement, content, 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Rename(replacement, path); err != nil {
						t.Fatal(err)
					}
				}
				options.StateCodec = probe
			}
			inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, options)
			if inventory.active != nil || err == nil && inventory.decision.State() != retirement.StateBlocked {
				t.Fatalf("inconsistent actual native inventory admitted: %v, %s", err, inventory.decision.Detail())
			}
		})
	}
}

func TestNativeInventoryPreservesBudgetCodecAndCancellationFailures(t *testing.T) {
	setup, root, _ := nativeInventoryFixture(t)
	options := inventoryOptions{Filesystem: journalTestFilesystem()}
	if inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, options); !errors.Is(err, errRecoveryJournalStateCodecRequired) || inventory.active != nil {
		t.Fatalf("missing native state codec was hidden as a layout decision: %v", err)
	}
	sentinel := errors.New("native inventory path budget refused")
	options.StateCodec = setup.fixture.codec
	options.PhysicalPathBudget = &nativeNamespaceBudget{err: sentinel}
	if inventory, err := loadNativeRecoveryRootInventory(t.Context(), root, options); !errors.Is(err, sentinel) || inventory.active != nil {
		t.Fatalf("native path-budget refusal lost its cause: %v", err)
	}
	options.PhysicalPathBudget = nil
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &nativeInventoryCodecProbe{SnapshotCodec: setup.fixture.codec, beforeDecode: cancel}
	options.StateCodec = probe
	if inventory, err := loadNativeRecoveryRootInventory(ctx, root, options); !errors.Is(err, context.Canceled) || inventory.active != nil || probe.decodes == 0 {
		t.Fatalf("cancellation during native state decode returned a prefix or lost its cause: %v", err)
	}
}

type nativeInventoryCodecProbe struct {
	durable.SnapshotCodec
	decodes      int
	beforeDecode func()
}

func (codec *nativeInventoryCodecProbe) Decode(content []byte) (durable.Snapshot, error) {
	codec.decodes++
	if codec.beforeDecode != nil {
		codec.beforeDecode()
	}
	return codec.SnapshotCodec.Decode(content)
}

func nativeInventoryFixture(t *testing.T) (nativeAssessmentFixture, string, string) {
	t.Helper()
	setup := newNativeAssessmentFixture(t)
	root := filepath.Join(t.TempDir(), "recovery")
	operationDir := filepath.Join(root, setup.fixture.record.OperationID)
	if err := os.MkdirAll(filepath.Join(operationDir, "images"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"before", "expected"} {
		content, err := os.ReadFile(filepath.Join(setup.fixture.record.ManifestRootProvenance.PhysicalRoot, "images", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(operationDir, "images", name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	artifacts, err := rootedpath.CaptureRoot(operationDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = artifacts.Close() })
	setup.fixture.read.artifacts = artifacts
	transition := fixtureTransition(t, setup.fixture)
	binding, _ := membershipBindingValue(t, setup.fixture.claims).Get()
	setup.persist(t, setup.fixture.before, assessmentRegistry(t, transition.Before(), binding), assessmentImage(t, setup.fixture, false))
	content, err := marshalNativeJournalRecord(t.Context(), setup.fixture.record, setup.fixture.before, setup.fixture.after, setup.fixture.codec)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(operationDir, recoveryJournalFileName)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return setup, root, path
}
