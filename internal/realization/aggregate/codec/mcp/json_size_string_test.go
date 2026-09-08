package mcpcodec

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRawJSONStringSizeMatchesStandardEncoderAtByteBoundaries(t *testing.T) {
	values := []string{
		`""`,
		`{"plain":"` + strings.Repeat("a", 256) + `","tail":[]}`,
		`{"literal":"<>&é\"\\` + "\u2028\u2029" + `","escaped":"\u2028\u2029\n\t"}`,
		` [ { "nested": [true, null, 1.25, " spaced "] } ] `,
		`"a€a€a€a€"`,
		string([]byte{'"', 'a', 0xe2, 'b', 0x80, 'c', 0xff, '"'}),
	}
	for value := range 256 {
		encoded, err := json.Marshal(string([]byte{byte(value)}))
		if err != nil {
			t.Fatal(err)
		}
		values = append(values, string(encoded))
	}

	for index, value := range values {
		for _, depth := range []int{0, 2} {
			t.Run(fmt.Sprintf("value-%d/depth-%d", index, depth), func(t *testing.T) {
				expected, err := json.MarshalIndent(json.RawMessage(value), strings.Repeat("  ", depth), "  ")
				if err != nil {
					t.Fatal(err)
				}
				for allowance := 0; allowance <= len(expected)+1; allowance++ {
					initial := maximumDocumentBytes - int64(allowance)
					counter := boundedCanonicalJSONSize{bytes: initial}
					err := counter.addRawJSON([]byte(value), depth)
					if allowance < len(expected) {
						if err == nil {
							t.Fatalf("allowance %d admitted %d encoded bytes", allowance, len(expected))
						}
						continue
					}
					if err != nil || counter.bytes-initial != int64(len(expected)) {
						t.Fatalf("allowance %d: measured %d/error %v, want %d encoded bytes", allowance, counter.bytes-initial, err, len(expected))
					}
				}
			})
		}
	}
}

func BenchmarkRawJSONStringSize(b *testing.B) {
	content := []byte(`{"value":"` + strings.Repeat("a", 1<<20) + `"}`)
	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	for b.Loop() {
		counter := boundedCanonicalJSONSize{}
		if err := counter.addRawJSON(content, 0); err != nil {
			b.Fatal(err)
		}
	}
}
