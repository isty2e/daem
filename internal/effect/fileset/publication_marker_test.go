package fileset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestPublicationMarkerBindsOriginalIntentBytes(t *testing.T) {
	absent := AbsentPublicationImage()
	intent, err := NewPublicationIntent([]byte("selected authority"), []PublicationTarget{
		publicationTargetForTest(t, filepath.Join(t.TempDir(), "state.json"), absent, publicationImageForTest(t, []byte("after"))),
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := encodePublicationIntent(intent)
	if err != nil {
		t.Fatal(err)
	}
	header, err := encodePublicationMarker(body)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := decodePublicationMarker(t.Context(), header)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := marker.decodeIntent(t.Context(), body); err != nil || !bytes.Equal(decoded.Selection(), intent.Selection()) {
		t.Fatal("marker did not identify its original containing intent", err)
	}
	var reformatted bytes.Buffer
	if err := json.Indent(&reformatted, body, "", "  "); err != nil {
		t.Fatal(err)
	}
	if _, err := marker.decodeIntent(t.Context(), reformatted.Bytes()); err == nil {
		t.Fatal("marker accepted different raw bytes with equivalent decoded facts")
	}
	var substituted publicationIntentRecord
	if err := json.Unmarshal(body, &substituted); err != nil {
		t.Fatal(err)
	}
	substituted.Selection[0] ^= 1
	changed, err := json.Marshal(substituted)
	if err != nil || len(changed) != len(body) {
		t.Fatal("substitution did not preserve the encoded size", err)
	}
	if _, err := marker.decodeIntent(t.Context(), changed); err == nil {
		t.Fatal("marker accepted a substituted same-size body")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := marker.decodeIntent(canceled, changed); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation was displaced by evidence validation", err)
	}
}

func TestPublicationMarkerRejectsIncompleteOrNonStrictDescriptors(t *testing.T) {
	digest := hashBytes([]byte("body"))
	for _, content := range [][]byte{
		[]byte(`{"version":5}`),
		[]byte(`{"version":5,"intent":null}`),
		[]byte(`{"version":5,"intent":{"size":1}}`),
		[]byte(`{"version":5,"intent":{"size":0,"hash":"` + digest + `"}}`),
		[]byte(`{"version":5,"intent":{"size":1,"hash":"invalid"}}`),
		[]byte(`{"version":5,"intent":{"size":1,"hash":"` + digest + `","unknown":true}}`),
		[]byte(`{"version":5,"version":5,"intent":{"size":1,"hash":"` + digest + `"}}`),
		[]byte(`{"version":6,"intent":{"size":1,"hash":"` + digest + `"}}`),
	} {
		if _, err := decodePublicationMarker(t.Context(), content); err == nil {
			t.Fatal("invalid publication marker was admitted")
		}
	}
	if _, err := encodePublicationMarker(nil); err == nil {
		t.Fatal("empty containing intent was admitted")
	}
}
