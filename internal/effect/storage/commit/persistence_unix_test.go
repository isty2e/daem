//go:build darwin || linux

package commit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestConfirmFilePersistenceSyncsWithoutReplacingOrRewriting(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "state.json")
	writeTestFile(t, path, "terminal bytes", 0o600)
	expected, err := CaptureEntryIdentity(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	var phases []phase
	err = confirmFilePersistenceWithFaults(t.Context(), path, expected, faultPlan{actions: map[phase]func(){
		phaseSyncPayload: func() { phases = append(phases, phaseSyncPayload) },
		phaseSyncParent:  func() { phases = append(phases, phaseSyncParent) },
		phaseWritePayload: func() {
			t.Fatal("persistence confirmation rewrote the target")
		},
	}})
	if err != nil || !slices.Equal(phases, []phase{phaseSyncPayload, phaseSyncParent}) {
		t.Fatal("file and parent completion were not ordered", phases, err)
	}
	current, err := CaptureEntryIdentity(t.Context(), path)
	if err != nil || !expected.Equal(current) {
		t.Fatal("confirmation replaced or changed the observed file", err)
	}
	assertFile(t, path, "terminal bytes", 0o600)
}

func TestConfirmFilePersistenceRetainsErrorsAndAllowsRetry(t *testing.T) {
	for _, failed := range []phase{phaseSyncPayload, phaseSyncParent} {
		t.Run(string(failed), func(t *testing.T) {
			path := filepath.Join(canonicalTempDir(t), "state.json")
			writeTestFile(t, path, "terminal bytes", 0o600)
			expected, err := CaptureEntryIdentity(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("injected persistence failure")
			parentCalled := false
			err = confirmFilePersistenceWithFaults(t.Context(), path, expected, faultPlan{
				failures: map[phase]error{failed: cause},
				actions:  map[phase]func(){phaseSyncParent: func() { parentCalled = true }},
			})
			if !errors.Is(err, cause) || parentCalled != (failed == phaseSyncParent) {
				t.Fatal("confirmation lost the failure or continued past payload failure", parentCalled, err)
			}
			assertFailure(t, err, failureRetainedResidue, failed)
			assertFile(t, path, "terminal bytes", 0o600)
			if err := ConfirmFilePersistence(t.Context(), path, expected); err != nil {
				t.Fatal("unchanged observed file could not be retried", err)
			}
		})
	}
}

func TestConfirmFilePersistenceRejectsStaleIdentityAndChangedBinding(t *testing.T) {
	for _, change := range []string{"before open", "during payload sync", "ancestor during parent sync"} {
		t.Run(change, func(t *testing.T) {
			root := canonicalTempDir(t)
			parent := filepath.Join(root, "parent")
			if err := os.Mkdir(parent, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(parent, "state.json")
			writeTestFile(t, path, "terminal bytes", 0o600)
			expected, err := CaptureEntryIdentity(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			replace := func() {
				t.Helper()
				replacement := filepath.Join(parent, "replacement")
				writeTestFile(t, replacement, "terminal bytes", 0o600)
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			faults := faultPlan{actions: make(map[phase]func())}
			switch change {
			case "before open":
				replace()
				faults.actions[phaseSyncPayload] = func() { t.Fatal("stale identity reached sync") }
			case "during payload sync":
				faults.actions[phaseSyncPayload] = replace
				faults.actions[phaseSyncParent] = func() { t.Fatal("changed entry reached parent sync") }
			case "ancestor during parent sync":
				faults.actions[phaseSyncParent] = func() {
					if err := os.Rename(parent, parent+"-moved"); err != nil {
						t.Fatal(err)
					}
					if err := os.Mkdir(parent, 0o700); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := confirmFilePersistenceWithFaults(t.Context(), path, expected, faults); err == nil {
				t.Fatal("changed identity or namespace was accepted")
			}
		})
	}
}

func TestConfirmFilePersistenceHonorsCancellation(t *testing.T) {
	path := filepath.Join(canonicalTempDir(t), "state.json")
	writeTestFile(t, path, "terminal bytes", 0o600)
	expected, err := CaptureEntryIdentity(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := ConfirmFilePersistence(ctx, path, expected); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled confirmation lost its cause", err)
	}
	if err := ConfirmFilePersistence(nil, path, expected); err == nil {
		t.Fatal("nil confirmation context accepted")
	}
}
