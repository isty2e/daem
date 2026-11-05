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

	"github.com/isty2e/daem/internal/effect/journal/recovery"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	journalqualification "github.com/isty2e/daem/internal/effect/journal/qualification"

	journalinventory "github.com/isty2e/daem/internal/effect/journal/inventory"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	"github.com/isty2e/daem/internal/effect/journal/retirement"
	"github.com/isty2e/daem/internal/effect/physicalwork"

	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/supply/artifact"
)

func TestNativeInventoryRetainsActualRecordAndFeedsDocumentAssessment(t *testing.T) {
	setup, root, path := nativeInventoryFixture(t)
	_, read := setup.reader(t)
	options := journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec, PhysicalPathBudget: read.Documents.Budget}
	inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, options)
	if err != nil || inventory.Decision().State() != retirement.StateActive || inventory.Active() == nil {
		t.Fatalf("native active inventory: %v, %s", err, inventory.Decision().Detail())
	}
	current, native := inventory.Active().Record().Native()
	if !native || !current.Before().Equal(setup.fixture.before) || !current.After().Equal(setup.fixture.after) {
		t.Fatal("native inventory lost its complete typed Snapshots")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Active().Identity().JournalAuthorityFingerprint() != journalrecord.Fingerprint(content) {
		t.Fatal("native inventory fingerprint differs from canonical full journal bytes")
	}
	if err := inventory.Active().PhysicalAuthority().Validate(); err != nil {
		t.Fatal(err)
	}
	assessment, err := journalqualification.AssessNativeDocuments(current, t.Context(), read)
	if err != nil || assessment.Facts().Classification() != recovery.ClassificationCleanBefore || assessment.Facts().DocumentCount() != 1 {
		t.Fatalf("native actual inventory-to-state/registry/image/host pipeline: %v", err)
	}
	if err := setup.fixture.read.Artifacts.ValidateSelectionBounded(inventory.Active().OperationDir(), physicalwork.MaximumPhysicalPathDepth, read.Documents.Budget); err != nil {
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
	inventory, err := journalinventory.LoadRoot(t.Context(), root, journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: codec})
	if err != nil || inventory.Decision().State() != retirement.StateBlocked || inventory.Active() != nil || codec.decodes != 0 {
		t.Fatalf("default inventory admitted native state or read it before refusal: %v, decodes=%d", err, codec.decodes)
	}
}

func TestNativeAndDefaultInventoryKeepGenerationIsolation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "recovery")
	identity, _ := captureInventoryJournal(t, root, "ordinary-native-reader")
	inventory, err := journalinventory.LoadRoot(t.Context(), root, journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: testStateCodec()})
	if err != nil || inventory.Active() == nil || inventory.Decision().State() != retirement.StateActive {
		t.Fatalf("default reader lost ordinary v15 recovery: %v", err)
	}
	legacy, ok := inventory.Active().Record().Ordinary()
	if !ok || legacy.OperationID() != identity.OperationID() || inventory.Active().Identity().JournalAuthorityFingerprint() != identity.JournalAuthorityFingerprint() {
		t.Fatal("default reader changed legacy record/fingerprint semantics")
	}
	native, err := statefile.NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	probe := &nativeInventoryCodecProbe{SnapshotCodec: native}
	rejected, err := journalinventory.LoadNativeRoot(t.Context(), root, journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: probe})
	if err != nil || rejected.Decision().State() != retirement.StateBlocked || rejected.Active() != nil || probe.decodes != 0 {
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
			identity, err := retirement.NewIdentity(setup.fixture.journal.OperationID(), journalrecord.Fingerprint(content))
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
					if err := os.Remove(filepath.Join(residue, journalrecord.FileName)); err != nil {
						t.Fatal(err)
					}
				}
			}
			inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec})
			if err != nil || inventory.Decision().State() != state || (inventory.Active() != nil) != (state == retirement.StatePrepared) {
				t.Fatalf("native control/residue state=%s want=%s: %v, %s", inventory.Decision().State(), state, err, inventory.Decision().Detail())
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
	identity, err := retirement.NewIdentity(setup.fixture.journal.OperationID(), journalrecord.Fingerprint(content))
	if err != nil {
		t.Fatal(err)
	}
	writeInventoryControl(t, root, identity, retirement.PhasePrepared)
	publishedNativeGroupVariant(t, setup.fixture, func(group map[string]json.RawMessage) {
		var original map[string]json.RawMessage
		if err := json.Unmarshal(group["original"], &original); err != nil {
			t.Fatal(err)
		}
		var reference map[string]json.RawMessage
		if err := json.Unmarshal(original["artifact"], &reference); err != nil {
			t.Fatal(err)
		}
		reference["digest"] = nativeRecordJSON(t, artifact.HashFileContent([]byte("different actual-image witness")))
		original["artifact"] = nativeRecordJSON(t, reference)
		group["original"] = nativeRecordJSON(t, original)
	})
	inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec})
	if err != nil || inventory.Decision().State() != retirement.StateBlocked || inventory.Active() != nil {
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
			inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: codec})
			if err != nil || inventory.Decision().State() != retirement.StateBlocked || inventory.Active() != nil || codec.decodes != 0 {
				t.Fatalf("unknown version reached state decoding or yielded active evidence: %v, %d", err, codec.decodes)
			}
		})
	}
}

func TestNativeInventoryRejectsInconsistentActiveLayouts(t *testing.T) {
	for _, kind := range []string{"directory name", "mixed families", "multiple native records", "record revision"} {
		t.Run(kind, func(t *testing.T) {
			setup, root, path := nativeInventoryFixture(t)
			options := journalinventory.Options{Filesystem: journalTestFilesystem(), StateCodec: setup.fixture.codec}
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
				publishedNativeVariant(t, other.fixture, func(frame map[string]json.RawMessage) {
					frame["operation_id"] = nativeRecordJSON(t, "another-native-operation")
				})
				if err := os.Rename(filepath.Dir(otherPath), filepath.Join(root, "another-native-operation")); err != nil {
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
			inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, options)
			if inventory.Active() != nil || err == nil && inventory.Decision().State() != retirement.StateBlocked {
				t.Fatalf("inconsistent actual native inventory admitted: %v, %s", err, inventory.Decision().Detail())
			}
		})
	}
}

func TestNativeInventoryPreservesBudgetCodecAndCancellationFailures(t *testing.T) {
	setup, root, _ := nativeInventoryFixture(t)
	options := journalinventory.Options{Filesystem: journalTestFilesystem()}
	if inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, options); !errors.Is(err, journalrecord.ErrStateCodecRequired) || inventory.Active() != nil {
		t.Fatalf("missing native state codec was hidden as a layout decision: %v", err)
	}
	sentinel := errors.New("native inventory path budget refused")
	options.StateCodec = setup.fixture.codec
	options.PhysicalPathBudget = &refusedWriterPathBudget{err: sentinel}
	if inventory, err := journalinventory.LoadNativeRoot(t.Context(), root, options); !errors.Is(err, sentinel) || inventory.Active() != nil {
		t.Fatalf("native path-budget refusal lost its cause: %v", err)
	}
	options.PhysicalPathBudget = nil
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	probe := &nativeInventoryCodecProbe{SnapshotCodec: setup.fixture.codec, beforeDecode: cancel}
	options.StateCodec = probe
	if inventory, err := journalinventory.LoadNativeRoot(ctx, root, options); !errors.Is(err, context.Canceled) || inventory.Active() != nil || probe.decodes == 0 {
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
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	operationDir := filepath.Join(root, setup.fixture.journal.OperationID())
	setup.destination = operationDir
	transition := *setup.groups[0].claims
	binding, _ := membershipBindingValue(t, setup.fixture.claims).Get()
	setup.persist(t, setup.fixture.before, assessmentRegistry(t, transition.Before(), binding), setup.image(false))
	path := filepath.Join(operationDir, journalrecord.FileName)
	return setup, root, path
}
