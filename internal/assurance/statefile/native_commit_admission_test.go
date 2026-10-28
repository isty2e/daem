package statefile_test

import (
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/assurance/statefile"
)

func TestNativePublicationIdentityDoesNotWidenLegacySchemas(t *testing.T) {
	encoded, err := (statefile.Codec{}).Encode(durable.EmptySnapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{8, 9, 10} {
		for _, value := range []string{"0", "1", "null", "-1", "1.5"} {
			var document map[string]json.RawMessage
			if err := json.Unmarshal(encoded, &document); err != nil {
				t.Fatal(err)
			}
			document["version"], _ = json.Marshal(version)
			document["native_document_commit"] = json.RawMessage(value)
			content, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			_, err = (statefile.Codec{}).Decode(content)
			admitted := version == 10 && (value == "0" || value == "1")
			if (err == nil) != admitted {
				t.Fatalf("native commit admission version=%d value=%s admitted=%v error=%v", version, value, admitted, err)
			}
		}
	}
}
