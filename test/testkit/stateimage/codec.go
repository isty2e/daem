package stateimage

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/pathauthority"
	"github.com/isty2e/daem/internal/assurance/statefile"
	"github.com/isty2e/daem/internal/effect/mutation"
)

type imageCodec struct{ reference pathauthority.Reference }

func Physical(t testing.TB, codec durable.SnapshotCodec) statefile.PhysicalCodec {
	t.Helper()
	physical, err := statefile.NewPhysicalCodec(codec)
	if err != nil {
		t.Fatal(err)
	}
	return physical
}

func NewCodec(t testing.TB, path string) statefile.PhysicalCodec {
	t.Helper()
	authority, err := mutation.ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	return Physical(t, imageCodec{reference: authority.Reference()})
}

func (codec imageCodec) Encode(snapshot durable.Snapshot) ([]byte, error) {
	image, err := statefile.NewStateImage(snapshot, codec.reference)
	if err != nil {
		return nil, err
	}
	return statefile.MarshalImage(image)
}

func (imageCodec) Decode(content []byte) (durable.Snapshot, error) {
	image, err := statefile.UnmarshalImage(content)
	if err != nil {
		return durable.Snapshot{}, err
	}
	return image.Snapshot(), nil
}

// CheckPublishedJournal compares its embedded State producer output, ignoring
// only the enclosing journal's additional JSON indentation. Absence is not success.
func CheckPublishedJournal(t testing.TB, directory string, codec durable.SnapshotCodec, before, after durable.Snapshot) bool {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(directory, entry.Name(), "journal.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var record struct {
			Before json.RawMessage `json:"statefile_before"`
			After  json.RawMessage `json:"statefile_after"`
		}
		if err := json.Unmarshal(content, &record); err != nil {
			t.Fatal(err)
		}
		for index, snapshot := range []durable.Snapshot{before, after} {
			actual := []json.RawMessage{record.Before, record.After}[index]
			expected, err := codec.Encode(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			var actualJSON, expectedJSON bytes.Buffer
			if err := json.Compact(&actualJSON, actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Compact(&expectedJSON, expected); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actualJSON.Bytes(), expectedJSON.Bytes()) {
				t.Fatalf("journal State[%d] differs from its frozen producer", index)
			}
		}
		return true
	}
	return false
}
