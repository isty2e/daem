package footprintcodec_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	footprintcodec "github.com/isty2e/daem/internal/encoding/footprint"
	"github.com/isty2e/daem/internal/output"
)

func TestFootprintRecordRoundTripsNativeIdentity(t *testing.T) {
	values := []output.Footprint{output.WholePathFootprint()}
	for _, keys := range [][]string{{""}, {"a/b", "~1", ""}, {"."}, {"line\nkey", "日本語"}} {
		path, err := output.NewKeyPath(keys...)
		if err != nil {
			t.Fatal(err)
		}
		value, err := output.NewKeyFootprint(path)
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	for _, value := range values {
		record, err := footprintcodec.Encode(value)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		var observed footprintcodec.Record
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&observed); err != nil {
			t.Fatal(err)
		}
		decoded, err := footprintcodec.Decode(observed)
		if err != nil || !decoded.Equal(value) {
			t.Fatalf("round trip lost scope: %s, %v", encoded, err)
		}
	}
}

func TestFootprintRecordRejectsInvalidVariantData(t *testing.T) {
	for _, encoded := range []string{
		`{}`, `{"kind":"unknown"}`, `{"kind":"key"}`, `{"kind":"key","key_path":null}`,
		`{"kind":"key","key_path":""}`, `{"kind":"key","key_path":"/bad~2"}`,
		`{"kind":"path","key_path":""}`, `{"kind":"path","key_path":"/a"}`,
	} {
		var record footprintcodec.Record
		if err := json.Unmarshal([]byte(encoded), &record); err != nil {
			t.Fatal(err)
		}
		if _, err := footprintcodec.Decode(record); !errors.Is(err, output.ErrInvalidFootprint) {
			t.Errorf("invalid record admitted: %s: %v", encoded, err)
		}
	}
	if _, err := footprintcodec.Encode(output.Footprint{}); !errors.Is(err, output.ErrInvalidFootprint) {
		t.Fatalf("invalid model encoded: %v", err)
	}
}

func TestFootprintRecordHasNoLegacyFieldFallback(t *testing.T) {
	for _, encoded := range []string{
		`{"content_path":"/a~1b"}`,
		`{"kind":"key","key_path":"/a","content_path":"/different"}`,
	} {
		decoder := json.NewDecoder(bytes.NewBufferString(encoded))
		decoder.DisallowUnknownFields()
		var record footprintcodec.Record
		if err := decoder.Decode(&record); err == nil {
			t.Fatalf("legacy fields entered current-format record: %s", encoded)
		}
	}
}
