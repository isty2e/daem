package journal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
)

func TestJournalV15RefusesNativeSnapshotsDespiteExplicitStateCodec(t *testing.T) {
	set := blockedJournalDesiredSet(t, "ordinary")
	parsed, err := configdocument.Parse(t.Context(), []byte(`{"value":true}`), configdocument.JSON, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	if err != nil {
		t.Fatal(err)
	}
	leaves, err := parsed.Leaves(t.Context(), 4096)
	if err != nil || len(leaves) != 1 {
		t.Fatalf("native value fixture differs: %v", err)
	}
	contribution, err := aggregate.NewManagedContribution(set.Contract(), leaves[0].Value.Canonical())
	if err != nil {
		t.Fatal(err)
	}
	nativeState, err := durable.NewManagedAggregateState(set.Contributions()[0].SubjectID(), contribution)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := aggregatecodec.ForContributions(t.Context(), []aggregate.CodecContractID{set.Contract().CodecContractID()}, configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 16})
	if err != nil {
		t.Fatal(err)
	}
	codec, err := statefile.NewCodec(catalog)
	if err != nil {
		t.Fatal(err)
	}
	for _, side := range []string{"before", "after"} {
		t.Run(side, func(t *testing.T) {
			journal := defaultRecoveryJournal()
			base := journal.StatefileBefore
			if side == "after" {
				base = journal.StatefileAfter
			}
			snapshot, err := durable.NewSnapshot(durable.SnapshotInput{ManagedPaths: base.ManagedPaths(), ManagedAggregates: []durable.ManagedAggregateState{nativeState}})
			if err != nil {
				t.Fatal(err)
			}
			if side == "before" {
				journal.StatefileBefore = snapshot
			} else {
				journal.StatefileAfter = snapshot
			}
			before, after, err := encodeRecoveryJournalSnapshots(journal.StatefileBefore, journal.StatefileAfter, codec)
			if err != nil {
				t.Fatalf("explicit state codec could not encode the native fixture: %v", err)
			}
			if err := validateRecoveryJournal(journal, codec); err == nil {
				t.Fatal("old journal validation admitted native baselines through a new state codec")
			}
			if content, err := marshalRecoveryJournalWithStateContent(journal, before, after); err == nil || len(content) != 0 {
				t.Fatal("old journal writer emitted native baselines")
			}

			content, err := encodeRecoveryJournal(journal, before, after)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "journal.json")
			if err := os.WriteFile(path, content, 0o600); err != nil {
				t.Fatal(err)
			}
			if loaded, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, codec); err == nil || loaded.Version != 0 || len(loaded.Entries) != 0 {
				t.Fatal("old journal reader admitted native baselines or returned partial authority")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, unchanged) {
				t.Fatalf("old-format refusal changed the journal artifact: %v", err)
			}
		})
	}
}
