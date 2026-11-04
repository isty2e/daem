//go:build darwin || linux

package commit

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
)

func readWorkCapability(t *testing.T, payload []byte, entries int, payloadBytes int64) (rootedpath.CommitCapability, *physicalwork.Budget, string) {
	t.Helper()
	path := filepath.Join(canonicalTempDir(t), "payload")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	root, destination, err := rootedpath.CaptureDestination(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	budget := physicalwork.NewCaptureBudget(nil)
	if err := budget.AdmitPhysicalWork(0, physicalwork.MaximumPhysicalEntries-entries, physicalwork.MaximumPhysicalBytes-payloadBytes); err != nil {
		t.Fatal(err)
	}
	capability, err := root.AcquireBounded(destination, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = capability.Close() })
	return capability, budget, path
}

func TestRootedWorkSnapshotChargesOnlyObservedPayload(t *testing.T) {
	for _, payload := range [][]byte{nil, bytes.Repeat([]byte("x"), 2*32*1024+3)} {
		capability, budget, _ := readWorkCapability(t, payload, 1, int64(len(payload)))
		content, mode, identity, err := ReadRootedRegularFileWithWorkBudget(t.Context(), capability, 1<<20)
		if err != nil || !bytes.Equal(content, payload) || mode != 0o600 || !identity.valid() {
			t.Fatalf("allocation-charged read: bytes=%d mode=%04o err=%v", len(content), mode, err)
		}
		if budget.RemainingEntries() != 0 || budget.RemainingBytes() != 0 {
			t.Fatal("read did not consume exactly one entry and its observed payload size")
		}
		if err := capability.Validate(); err != nil {
			t.Fatal("read consumed the caller's capability", err)
		}
	}
}

func TestRootedWorkSnapshotRefusesBeforeUnadmittedPayload(t *testing.T) {
	for _, allowance := range []struct {
		name    string
		entries int
		bytes   int64
	}{
		{name: "entry", entries: 0, bytes: 7},
		{name: "payload", entries: 1, bytes: 6},
	} {
		t.Run(allowance.name, func(t *testing.T) {
			capability, _, path := readWorkCapability(t, []byte("payload"), allowance.entries, allowance.bytes)
			var reads int
			faults := faultPlan{payloadRead: func(buffer []byte, read func([]byte) (int, error)) (int, error) {
				reads++
				return read(buffer)
			}}
			limit := int64(1 << 20)
			content, _, _, err := readRegularFileSnapshotWithin(t.Context(), path, capability, &limit, faults, true)
			if err == nil || content != nil || reads != 0 {
				t.Fatalf("unadmitted payload transferred: reads=%d err=%v", reads, err)
			}
			assertFailure(t, err, failureUncommitted, phaseReadPayload)
			if err := capability.Validate(); err != nil {
				t.Fatal("failed read consumed the caller's capability", err)
			}
		})
	}
}

func TestRootedWorkSnapshotGrowthCannotSpendUnreservedBytes(t *testing.T) {
	capability, budget, path := readWorkCapability(t, []byte("old"), 1, 3)
	transferred := 0
	grew := false
	faults := faultPlan{
		actions: map[phase]func(){phaseReadPayload: func() {
			if !grew {
				if err := os.WriteFile(path, []byte("expanded"), 0o600); err != nil {
					t.Fatal(err)
				}
				grew = true
			}
		}},
		payloadRead: func(buffer []byte, read func([]byte) (int, error)) (int, error) {
			if len(buffer) > 3-transferred {
				t.Fatal("read request exceeds the prepaid observed size")
			}
			count, err := read(buffer)
			transferred += count
			return count, err
		},
	}
	limit := int64(1 << 20)
	content, _, _, err := readRegularFileSnapshotWithin(t.Context(), path, capability, &limit, faults, true)
	if !grew || !errors.Is(err, ErrRegularFileReadLimitExceeded) || content != nil || transferred > 3 {
		t.Fatalf("unadmitted growth: grew=%t transferred=%d err=%v", grew, transferred, err)
	}
	if budget.RemainingBytes() != 0 {
		t.Fatal("failed read refunded its prepaid payload")
	}
}
