//go:build darwin || linux

package statefile

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestPhysicalStateRetainsRecordedAliasAndGuardsUnrelatedReferenceChanges(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "state.json")
	observed := statePhysicalReference(t, path)
	alias := filepath.Join(root, "lookup")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	recorded, err := pathauthority.NewReference(observed.Identity(), filepath.Join(alias, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	before := durable.EmptySnapshot()
	content := writePhysicalStateImage(t, path, before, recorded)
	state, err := LoadOptionalPhysicalWithOptions(t.Context(), path, ReadOptions{Codec: DefaultPhysicalCodec(), PhysicalPathBudget: physicalwork.NewPathBudget()})
	if err != nil || !state.Snapshot().Equal(before) {
		t.Fatal("physical image read lost canonical facts", err)
	}
	next, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 7})
	if err != nil {
		t.Fatal(err)
	}
	after, err := state.Codec().EncodeState(next)
	if err != nil {
		t.Fatal(err)
	}
	image, err := UnmarshalImage(after)
	if err != nil || !image.Snapshot().Equal(next) || !image.Reference().Equal(recorded) {
		t.Fatal("physical update did not retain its recorded alias", err)
	}
	for _, input := range [][]byte{content, after} {
		if _, err := state.Codec().DecodeState(input); err != nil {
			t.Fatal("selected codec rejected its own Before or successor", err)
		}
	}
	unrelated, err := NewStateImage(before, observed)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := MarshalImage(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	var stale mutation.StaleSnapshotError
	if _, err := state.Codec().DecodeState(changed); !errors.As(err, &stale) {
		t.Fatal("same-facts unrelated locator change was not stale", err)
	}
	frozen, err := Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Codec().DecodeState(frozen); !errors.As(err, &stale) {
		t.Fatal("located-to-legacy format change was not stale", err)
	}
	if _, err := (Codec{}).Decode(after); err == nil {
		t.Fatal("physical adoption changed the frozen Snapshot decoder")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	brokenAlias, err := LoadOptionalPhysical(t.Context(), path)
	if err != nil {
		t.Fatal("unused broken lookup alias blocked a selected-path read", err)
	}
	retained, err := brokenAlias.Codec().EncodeState(before)
	if err != nil || !bytes.Equal(retained, content) {
		t.Fatal("broken alias was followed or silently replaced", err)
	}
}

func TestMovedOrdinaryStateAcceptsBeforeAndSuccessorPhysicalReferences(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldPath, newPath := filepath.Join(root, "old.json"), filepath.Join(root, "new.json")
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{NativeDocumentCommit: 3})
	if err != nil {
		t.Fatal(err)
	}
	before := writePhysicalStateImage(t, oldPath, snapshot, statePhysicalReference(t, oldPath))
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	state, err := LoadOptionalPhysical(t.Context(), newPath)
	if err != nil || !state.Snapshot().Equal(snapshot) {
		t.Fatal("ordinary State acquired an immovability restriction", err)
	}
	after, err := state.Codec().EncodeState(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	image, err := UnmarshalImage(after)
	if err != nil || !image.Reference().Equal(statePhysicalReference(t, newPath)) {
		t.Fatal("moved State did not select its observed current reference", err)
	}
	for _, content := range [][]byte{before, after} {
		if loaded, err := state.Codec().DecodeState(content); err != nil || !loaded.Equal(snapshot) {
			t.Fatal("moved State failed Before/post-commit/settlement decoding", err)
		}
	}
	if _, err := os.Lstat(oldPath); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("physical read followed or recreated the old State locator", err)
	}
}

func TestPhysicalStateEnvelopeUsesTheOperationSelectedFactCatalog(t *testing.T) {
	jsonState := contributionCodecState(t, configdocument.JSON)
	tomlState := contributionCodecState(t, configdocument.TOML)
	snapshot := contributionCodecSnapshot(t, jsonState, tomlState)
	path := filepath.Join(t.TempDir(), "state.json")
	content := writePhysicalStateImage(t, path, snapshot, statePhysicalReference(t, path))
	state, err := LoadPhysical(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	jsonOnly := contributionStateCodec(t, t.Context(), []aggregate.CodecContractID{jsonState.Contribution().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	template, err := NewPhysicalCodec(jsonOnly)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := template.ForObservedState(state.Codec())
	if err != nil {
		t.Fatal(err)
	}
	if output, err := selected.EncodeState(snapshot); err == nil || len(output) != 0 {
		t.Fatal("image encoding fell back to the default broader catalog")
	}
	if loaded, err := selected.DecodeState(content); err == nil || len(loaded.ManagedAggregates()) != 0 {
		t.Fatal("image decoding fell back to the default broader catalog")
	}
	if _, err := LoadOptionalPhysicalWithOptions(t.Context(), path, ReadOptions{Codec: template, PhysicalPathBudget: physicalwork.NewPathBudget()}); err == nil {
		t.Fatal("physical reader bypassed its selected contribution validators")
	}
}

func TestPhysicalStateMissingAndLegacyKeepTheirDefaultWithoutMaskingObservationFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	frozen, err := Marshal(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, exists := range []bool{false, true} {
		if exists {
			if err := os.WriteFile(path, frozen, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		state, err := LoadOptionalPhysical(t.Context(), path)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := state.Codec().EncodeState(state.Snapshot())
		if err != nil || !bytes.Equal(encoded, frozen) {
			t.Fatal("observation activated a new physical format", err)
		}
		image, err := NewStateImage(durable.EmptySnapshot(), statePhysicalReference(t, path))
		if err != nil {
			t.Fatal(err)
		}
		intervening, err := MarshalImage(image)
		if err != nil {
			t.Fatal(err)
		}
		var stale mutation.StaleSnapshotError
		if _, err := state.Codec().DecodeState(intervening); !errors.As(err, &stale) {
			t.Fatal("legacy or absent observation silently adopted a later image", err)
		}
	}
	if _, err := LoadOptionalPhysicalWithOptions(t.Context(), path, ReadOptions{Codec: DefaultPhysicalCodec(), PhysicalPathBudget: selectedStateBudget{fs.ErrNotExist}}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("budget cause was not preserved", err)
	}
}

func statePhysicalReference(t *testing.T, path string) pathauthority.Reference {
	t.Helper()
	observed, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	return observed.Reference()
}

func writePhysicalStateImage(t *testing.T, path string, snapshot durable.Snapshot, reference pathauthority.Reference) []byte {
	t.Helper()
	image, err := NewStateImage(snapshot, reference)
	if err != nil {
		t.Fatal(err)
	}
	content, err := MarshalImage(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return content
}
