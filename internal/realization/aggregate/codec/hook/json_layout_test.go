package hookcodec_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/isty2e/daem/internal/realization/aggregate"
)

func TestHookJSONLayoutUsesContainingDocumentForMissingArea(t *testing.T) {
	placement, codec, selection := hookCodecFixture(t)
	desired := hookContributionSet(t, placement, hookContributionSpec{name: "stop", event: "Stop", command: "updated"})
	for _, source := range []string{"{\n  \"foreign\": 1e+02\n}\n", "{\n  \"foreign\": 1e+02,\n  \"hooks\": {}\n}\n"} {
		original := aggregate.ExistingDocument([]byte(source))
		rendered := renderHookSet(t, codec, selection, original, desired)
		var root, events map[string]json.RawMessage
		if err := json.Unmarshal(rendered.Content(), &root); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(root["hooks"], &events); err != nil {
			t.Fatal(err)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, events["Stop"]); err != nil {
			t.Fatal(err)
		}
		want := "{\n    \"Stop\": " + compact.String() + "\n  }"
		if string(root["hooks"]) != want || string(root["foreign"]) != "1e+02" {
			t.Fatalf("hook projection ignored containing style or outside bytes: %s", rendered.Content())
		}
		stable := renderHookSet(t, codec, selection, rendered, desired)
		if !bytes.Equal(stable.Content(), rendered.Content()) {
			t.Fatal("identical hook request changed concrete layout")
		}
	}
}
