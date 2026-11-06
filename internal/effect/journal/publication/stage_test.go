package publication

import (
	"errors"
	"io"
	"io/fs"
	"testing"

	journalrecord "github.com/isty2e/daem/internal/effect/journal/record"
	mutationfs "github.com/isty2e/daem/internal/effect/mutation/filesystem"
	"github.com/isty2e/daem/internal/effect/workmeasure"
)

func TestUninitializedJournalCannotStageAPrefix(t *testing.T) {
	for name, prepared := range map[string]PreparedJournal{
		"zero":           {},
		"unpaired bytes": {content: []byte("not a completed record")},
	} {
		t.Run(name, func(t *testing.T) {
			writer := &rejectingStageWriter{}
			fingerprint, err := prepared.Stage(t.Context(), writer)
			if err == nil || fingerprint != "" || writer.writes != 0 {
				t.Fatalf("uninitialized staging exposed a prefix: fingerprint=%q writes=%d err=%v", fingerprint, writer.writes, err)
			}
		})
	}
	if value, err := PrepareJournal(journalrecord.Native{}, nil, nil, StageOptions{}, workmeasure.ArtifactWork{}); err == nil {
		t.Fatal("missing preparation context produced staging work", value)
	}
}

type rejectingStageWriter struct {
	mutationfs.RootedTreeWriter
	writes int
}

func (writer *rejectingStageWriter) CreateDirectory(mutationfs.TreeRelativePath, fs.FileMode) error {
	writer.writes++
	return errors.New("unexpected staged directory")
}

func (writer *rejectingStageWriter) WriteFile(mutationfs.TreeRelativePath, fs.FileMode, io.Reader) error {
	writer.writes++
	return errors.New("unexpected staged file")
}
