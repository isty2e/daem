package retirement

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestRecordGoldenEncodingAndRoundTrip(t *testing.T) {
	record := mustRecord(t, testOperationID, testFingerprint, PhasePrepared)
	want := []byte("{\n" +
		"  \"version\": 1,\n" +
		"  \"phase\": \"prepared\",\n" +
		"  \"operation_id\": \"20260730T120000.000000000Z-apply\",\n" +
		"  \"journal_authority_fingerprint\": \"sha256:1111111111111111111111111111111111111111111111111111111111111111\"\n" +
		"}\n")

	encoded, err := Encode(record)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("Encode bytes:\n%s\nwant:\n%s", encoded, want)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	if !decoded.Identity().equal(record.Identity()) || decoded.Phase() != record.Phase() {
		t.Fatalf("Decode = %#v, want %#v", decoded, record)
	}
}

func TestRecordPhaseAdvancePreservesIdentityAndNames(t *testing.T) {
	prepared := mustRecord(t, testOperationID, testFingerprint, PhasePrepared)
	finalizing, err := prepared.Finalizing()
	if err != nil {
		t.Fatalf("Finalizing returned error: %v", err)
	}
	again, err := finalizing.Finalizing()
	if err != nil {
		t.Fatalf("second Finalizing returned error: %v", err)
	}

	if finalizing.Phase() != PhaseFinalizing || again.Phase() != PhaseFinalizing {
		t.Fatalf("finalizing phases = %q and %q", finalizing.Phase(), again.Phase())
	}
	if !prepared.Identity().equal(finalizing.Identity()) ||
		!finalizing.Identity().equal(again.Identity()) {
		t.Fatal("phase advance changed immutable identity")
	}
	if prepared.Identity().ControlName() != finalizing.Identity().ControlName() ||
		prepared.Identity().ResidueName() != finalizing.Identity().ResidueName() ||
		prepared.Identity().GCName() != finalizing.Identity().GCName() {
		t.Fatal("phase advance changed correlated names")
	}
}

func TestDecodeRejectsMalformedRecord(t *testing.T) {
	valid := fmt.Sprintf(
		`{"version":1,"phase":"prepared","operation_id":%q,"journal_authority_fingerprint":%q}`,
		testOperationID,
		testFingerprint,
	)
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "empty", content: nil},
		{name: "unknown field", content: []byte(strings.TrimSuffix(valid, "}") + `,"unknown":true}`)},
		{name: "missing version", content: []byte(fmt.Sprintf(
			`{"phase":"prepared","operation_id":%q,"journal_authority_fingerprint":%q}`,
			testOperationID,
			testFingerprint,
		))},
		{name: "missing phase", content: []byte(fmt.Sprintf(
			`{"version":1,"operation_id":%q,"journal_authority_fingerprint":%q}`,
			testOperationID,
			testFingerprint,
		))},
		{name: "missing operation id", content: []byte(fmt.Sprintf(
			`{"version":1,"phase":"prepared","journal_authority_fingerprint":%q}`,
			testFingerprint,
		))},
		{name: "missing fingerprint", content: []byte(fmt.Sprintf(
			`{"version":1,"phase":"prepared","operation_id":%q}`,
			testOperationID,
		))},
		{name: "duplicate key", content: []byte(fmt.Sprintf(
			`{"version":1,"version":1,"phase":"prepared","operation_id":%q,"journal_authority_fingerprint":%q}`,
			testOperationID,
			testFingerprint,
		))},
		{name: "future version", content: []byte(strings.Replace(valid, `"version":1`, `"version":2`, 1))},
		{name: "unknown phase", content: []byte(strings.Replace(valid, `"phase":"prepared"`, `"phase":"done"`, 1))},
		{name: "reserved operation id", content: []byte(strings.Replace(
			valid,
			testOperationID,
			"retirement-v1-"+testDigest,
			1,
		))},
		{name: "bad fingerprint", content: []byte(strings.Replace(
			valid,
			testFingerprint,
			"sha256:"+strings.Repeat("A", 64),
			1,
		))},
		{name: "wrong version type", content: []byte(strings.Replace(valid, `"version":1`, `"version":"1"`, 1))},
		{name: "trailing value", content: []byte(valid + ` {}`)},
		{name: "trailing garbage", content: []byte(valid + ` garbage`)},
		{name: "invalid utf8", content: append([]byte(valid), 0xff)},
		{name: "nested object", content: []byte(strings.Replace(valid, `"phase":"prepared"`, `"phase":{"value":"prepared"}`, 1))},
		{name: "oversized", content: bytes.Repeat([]byte{' '}, MaximumRecordBytes+1)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Decode(test.content); err == nil {
				t.Fatalf("Decode(%q) succeeded", test.content)
			}
		})
	}
}

func TestDecodeAcceptsMaximumSizedDocumentAndCanonicalizesIt(t *testing.T) {
	record := mustRecord(t, testOperationID, testFingerprint, PhasePrepared)
	canonical, err := Encode(record)
	if err != nil {
		t.Fatalf("Encode returned error: %v", err)
	}
	content := append(append([]byte(nil), canonical...), bytes.Repeat(
		[]byte{' '},
		MaximumRecordBytes-len(canonical),
	)...)

	decoded, err := Decode(content)
	if err != nil {
		t.Fatalf("Decode maximum-sized document returned error: %v", err)
	}
	reencoded, err := Encode(decoded)
	if err != nil {
		t.Fatalf("Encode decoded record returned error: %v", err)
	}
	if !bytes.Equal(reencoded, canonical) {
		t.Fatalf("re-encoded bytes differ:\n%s\nwant:\n%s", reencoded, canonical)
	}
}

func TestEncodeRejectsOversizedCanonicalRecord(t *testing.T) {
	if _, err := NewRecord(
		strings.Repeat("a", MaximumRecordBytes),
		testFingerprint,
		PhasePrepared,
	); err == nil {
		t.Fatal("NewRecord admitted an unencodable oversized record")
	}
}

func TestRecordRejectsUnknownPhaseAndZeroValue(t *testing.T) {
	if _, err := NewRecord(testOperationID, testFingerprint, Phase("unknown")); err == nil {
		t.Fatal("NewRecord admitted unknown phase")
	}
	if _, err := (Record{}).Finalizing(); err == nil {
		t.Fatal("zero Record.Finalizing succeeded")
	}
	if _, err := Encode(Record{}); err == nil {
		t.Fatal("Encode zero record succeeded")
	}
}

func mustRecord(t *testing.T, operationID string, fingerprint string, phase Phase) Record {
	t.Helper()

	record, err := NewRecord(operationID, fingerprint, phase)
	if err != nil {
		t.Fatalf("NewRecord returned error: %v", err)
	}
	return record
}
