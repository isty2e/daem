package record

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/target"
)

func TestJournalV15RefusesNativeDocumentsDespiteNativeStateCodec(t *testing.T) {
	codec, err := statefile.NewNativeCodec(aggregatecodec.Catalog().Contributions())
	if err != nil {
		t.Fatal(err)
	}
	for _, side := range []string{"before", "after"} {
		t.Run(side, func(t *testing.T) {
			journal := defaultRecoveryJournal()
			baseline := nativeWholeFileJournalBaseline(t, target.TargetPi, target.ScopeGlobal)
			if side == "before" {
				journal.StatefileBefore = statefileFor(append(journal.StatefileBefore.ManagedPaths(), baseline)...)
			} else {
				journal.StatefileAfter = statefileFor(append(journal.StatefileAfter.ManagedPaths(), baseline)...)
			}
			before, after, err := EncodeSnapshots(journal.StatefileBefore, journal.StatefileAfter, codec)
			if err != nil {
				t.Fatalf("real native state codec rejected the fixture: %v", err)
			}
			if err := validateRecoveryJournal(journal, codec); err == nil {
				t.Fatal("native state capability widened legacy journal admission")
			}
			if content, err := marshalRecoveryJournalWithStateContent(journal, before, after); err == nil || len(content) != 0 {
				t.Fatal("legacy writer emitted native state")
			}

			content, err := encodeRecoveryJournal(journal, before, after)
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
			if result, err := loadRecoveryJournal(t.Context(), journalTestFilesystem(), path, codec); err == nil || result.Version != 0 || len(result.Entries) != 0 || !result.StatefileBefore.Equal(durable.EmptySnapshot()) || !result.StatefileAfter.Equal(durable.EmptySnapshot()) {
				t.Fatal("legacy reader returned native or partial recovery authority")
			}
			unchanged, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(content, unchanged) {
				t.Fatalf("legacy refusal changed journal evidence: %v", err)
			}
		})
	}
}
