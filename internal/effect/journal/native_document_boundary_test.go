package journal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/test/outputtest"
)

func TestJournalV15RefusesNativeWholeFileSnapshotsBeforeStateEncoding(t *testing.T) {
	for _, selectedTarget := range []target.Target{target.TargetPi, target.TargetCodex} {
		for _, scope := range []target.Scope{target.ScopeGlobal, target.ScopeProject} {
			for _, side := range []string{"before", "after"} {
				t.Run(fmt.Sprintf("%s/%s/%s", selectedTarget, scope, side), func(t *testing.T) {
					journal := defaultRecoveryJournal()
					baseline := nativeWholeFileJournalBaseline(t, selectedTarget, scope)
					if side == "before" {
						journal.StatefileBefore = statefileFor(append(journal.StatefileBefore.ManagedPaths(), baseline)...)
					} else {
						journal.StatefileAfter = statefileFor(append(journal.StatefileAfter.ManagedPaths(), baseline)...)
					}
					codec := &nativeWholeFileFixtureCodec{before: journal.StatefileBefore, after: journal.StatefileAfter}

					if err := validateRecoveryJournal(journal, codec); err == nil || codec.encodeCalls != 0 {
						t.Fatalf("legacy validation accepted native ownership or reached state encoding: error=%v calls=%d", err, codec.encodeCalls)
					}
					if content, err := marshalRecoveryJournalWithStateContent(journal, []byte(`{"phase":"before"}`), []byte(`{"phase":"after"}`)); err == nil || len(content) != 0 {
						t.Fatal("legacy writer emitted native whole-file ownership")
					}

					content, err := encodeRecoveryJournal(journal, []byte(`{"phase":"before"}`), []byte(`{"phase":"after"}`))
					if err != nil {
						t.Fatal(err)
					}
					directory, err := filepath.EvalSymlinks(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					path := filepath.Join(directory, "journal.json")
					if err := os.WriteFile(path, content, 0o600); err != nil {
						t.Fatal(err)
					}
					codec.encodeCalls = 0
					if loaded, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, codec); err == nil || loaded.Version != 0 || len(loaded.Entries) != 0 || codec.encodeCalls != 0 {
						t.Fatalf("legacy reader returned native authority or reencoded unsupported state: error=%v version=%d entries=%d calls=%d", err, loaded.Version, len(loaded.Entries), codec.encodeCalls)
					}
					unchanged, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(content, unchanged) {
						t.Fatalf("native-family refusal changed the journal artifact: %v", err)
					}
				})
			}
		}
	}
}

func TestJournalV15RetainsOrdinaryWholeFileSnapshotsWithSelectedStateCodec(t *testing.T) {
	journal := defaultRecoveryJournal()
	codec := &nativeWholeFileFixtureCodec{before: journal.StatefileBefore, after: journal.StatefileAfter}
	if err := validateRecoveryJournal(journal, codec); err != nil {
		t.Fatalf("ordinary journal validation: %v", err)
	}
	content, err := marshalRecoveryJournalWithStateContent(journal, []byte(`{"phase":"before"}`), []byte(`{"phase":"after"}`))
	if err != nil {
		t.Fatalf("ordinary journal encoding: %v", err)
	}
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "journal.json")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, codec)
	if err != nil {
		t.Fatalf("ordinary journal decoding: %v", err)
	}
	if loaded.Version != journal.Version || !loaded.StatefileBefore.Equal(journal.StatefileBefore) || !loaded.StatefileAfter.Equal(journal.StatefileAfter) {
		t.Fatal("legacy journal did not preserve ordinary snapshot authority")
	}
}

func nativeWholeFileJournalBaseline(t *testing.T, selectedTarget target.Target, scope target.Scope) durable.ManagedPathState {
	t.Helper()
	id, err := entity.New(entity.KindHostConfig, "native")
	if err != nil {
		t.Fatal(err)
	}
	subject, err := aggregate.HostConfigDocumentSubject(id, selectedTarget, scope)
	if err != nil {
		t.Fatal(err)
	}
	root := "." + string(selectedTarget)
	if scope == target.ScopeGlobal {
		root = "@codex-home"
		if selectedTarget == target.TargetPi {
			root = "@pi-agent"
		}
	}
	baseline, err := durable.NewManagedPathState(subject, []target.Target{selectedTarget}, scope,
		outputtest.Parse(t, root+"/extension-settings/native.json"), testContentHash("native"),
		realization.PathProjectionFile, realization.PathPermissionsExact, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	return baseline
}

// The selected codec isolates journal family admission from profile admission.
// It models a future state codec accepting these already-valid domain rows.
type nativeWholeFileFixtureCodec struct {
	before      durable.Snapshot
	after       durable.Snapshot
	encodeCalls int
}

func (codec *nativeWholeFileFixtureCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	codec.encodeCalls++
	if snapshot.Equal(codec.before) {
		return []byte(`{"phase":"before"}`), nil
	}
	if snapshot.Equal(codec.after) {
		return []byte(`{"phase":"after"}`), nil
	}
	return nil, fmt.Errorf("unexpected fixture snapshot")
}

func (codec *nativeWholeFileFixtureCodec) Decode(content []byte) (durable.Snapshot, error) {
	var marker struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(content, &marker); err != nil {
		return durable.Snapshot{}, err
	}
	switch marker.Phase {
	case "before":
		return codec.before, nil
	case "after":
		return codec.after, nil
	default:
		return durable.Snapshot{}, fmt.Errorf("unexpected fixture state content")
	}
}
