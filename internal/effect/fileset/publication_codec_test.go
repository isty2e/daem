package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func TestPublicationIntentCodecPreservesExactFacts(t *testing.T) {
	root := t.TempDir()
	before := publicationImageForTest(t, []byte("before"))
	after := publicationImageForTest(t, []byte("after"))
	absent := AbsentPublicationImage()
	intent, err := NewPublicationIntent([]byte{0, 255, 's'}, []PublicationTarget{
		publicationTargetForTest(t, filepath.Join(root, "retained.json"), before, before),
		publicationTargetForTest(t, filepath.Join(root, "changed.json"), before, after),
		publicationTargetForTest(t, filepath.Join(root, "created.json"), absent, after),
		publicationTargetForTest(t, filepath.Join(root, "absent.json"), absent, absent),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := encodePublicationIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := decodePublicationIntent(t.Context(), content)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Selection(), intent.Selection()) || !slices.Equal(restored.Targets(), intent.Targets()) {
		t.Fatal("publication codec changed selection, target order or image facts")
	}
	encodedAgain, err := encodePublicationIntent(restored)
	if err != nil || !bytes.Equal(encodedAgain, content) {
		t.Fatal("canonical publication intent did not re-encode identically", err)
	}
}

func TestPublicationIntentCodecRejectsMalformedEvidence(t *testing.T) {
	image := publicationImageForTest(t, []byte("a"))
	path := filepath.Join(t.TempDir(), "state.json")
	for _, test := range []struct {
		name   string
		change func(*publicationIntentRecord)
	}{
		{name: "old version", change: func(record *publicationIntentRecord) { record.Version = 4 }},
		{name: "future version", change: func(record *publicationIntentRecord) { record.Version = 6 }},
		{name: "selection", change: func(record *publicationIntentRecord) { record.Selection = nil }},
		{name: "targets", change: func(record *publicationIntentRecord) { record.Targets = nil }},
		{name: "duplicate target", change: func(record *publicationIntentRecord) { record.Targets = append(record.Targets, record.Targets[0]) }},
		{name: "before", change: func(record *publicationIntentRecord) { record.Targets[0].Before = nil }},
		{name: "exists", change: func(record *publicationIntentRecord) { record.Targets[0].Before.Exists = nil }},
		{name: "size", change: func(record *publicationIntentRecord) { record.Targets[0].Before.Size = nil }},
		{name: "hash", change: func(record *publicationIntentRecord) { record.Targets[0].Before.Hash = nil }},
		{name: "mode", change: func(record *publicationIntentRecord) { record.Targets[0].Before.Mode = nil }},
		{name: "negative size", change: func(record *publicationIntentRecord) { *record.Targets[0].Before.Size = -1 }},
		{name: "inconsistent empty image", change: func(record *publicationIntentRecord) { *record.Targets[0].Before.Size = 0 }},
		{name: "bad hash", change: func(record *publicationIntentRecord) { *record.Targets[0].Before.Hash = "invalid" }},
		{name: "bad mode", change: func(record *publicationIntentRecord) { *record.Targets[0].Before.Mode = 0o1000 }},
		{name: "absent facts", change: func(record *publicationIntentRecord) { *record.Targets[0].Before.Exists = false }},
		{name: "deletion", change: func(record *publicationIntentRecord) {
			record.Targets[0].After = publicationRecordForImage(AbsentPublicationImage())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := publicationIntentRecord{
				Version: 5, Selection: []byte("selection"), Targets: []publicationTargetRecord{
					{Path: path, Before: publicationRecordForImage(image), After: publicationRecordForImage(image)},
				},
			}
			test.change(&record)
			content, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if result, err := decodePublicationIntent(t.Context(), content); err == nil || result.Validate() == nil {
				t.Fatal("malformed publication evidence returned admitted intent", err)
			}
		})
	}
}

func TestPublicationIntentCodecPreservesStrictJSONAndCancellation(t *testing.T) {
	absent := AbsentPublicationImage()
	intent, err := NewPublicationIntent([]byte("selection"), []PublicationTarget{
		publicationTargetForTest(t, filepath.Join(t.TempDir(), "state.json"), absent, absent),
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := encodePublicationIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range [][]byte{
		append(slices.Clone(content), []byte("{}")...),
		bytes.Replace(content, []byte(`"version":5`), []byte(`"version":5,"version":5`), 1),
		bytes.Replace(content, []byte(`"version":5`), []byte(`"version":5,"unknown":true`), 1),
		bytes.Replace(content, []byte(`"size":0,`), nil, 1),
	} {
		if _, err := decodePublicationIntent(t.Context(), malformed); err == nil {
			t.Fatal("non-strict publication record was admitted")
		}
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := decodePublicationIntent(canceled, []byte("malformed")); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was replaced with a parse failure", err)
	}
}

func TestPublicationIntentBoundsEncodedRecord(t *testing.T) {
	absent := AbsentPublicationImage()
	target := publicationTargetForTest(t, filepath.Join(t.TempDir(), "state.json"), absent, absent)
	small, err := NewPublicationIntent(make([]byte, 3), []PublicationTarget{target})
	if err != nil {
		t.Fatal(err)
	}
	smallContent, err := encodePublicationIntent(small)
	if err != nil {
		t.Fatal(err)
	}
	overhead := int64(len(smallContent) - 4)
	admittedLength := int((maximumTargetBytes - overhead) / 4 * 3)
	selection := make([]byte, admittedLength+3)
	if _, err := NewPublicationIntent(selection[:admittedLength], []PublicationTarget{target}); err != nil {
		t.Fatal("containing intent immediately below the encoded limit was refused", err)
	}
	if _, err := NewPublicationIntent(selection, []PublicationTarget{target}); err == nil {
		t.Fatal("encoded intent byte limit was lost")
	}
	oversized, err := json.Marshal(publicationIntentRecord{
		Version: 5, Selection: selection, Targets: []publicationTargetRecord{
			{Path: target.Path(), Before: publicationRecordForImage(absent), After: publicationRecordForImage(absent)},
		},
	})
	if err != nil || int64(len(oversized)) <= maximumTargetBytes {
		t.Fatal("oversized fixture did not exceed encoded limit", err)
	}
	if decoded, err := decodePublicationIntent(t.Context(), oversized); err == nil || decoded.Validate() == nil {
		t.Fatal("oversized containing intent returned admitted state", err)
	}
}
