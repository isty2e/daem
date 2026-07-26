package archguard

import "testing"

func TestParseGoListJSONReadsConcatenatedPackageObjects(t *testing.T) {
	data := []byte(`{"ImportPath":"example.com/project/internal/resource/skill","Imports":["fmt"]}
{"ImportPath":"example.com/project/internal/output","GoFiles":["output.go"]}`)

	records, err := ParseGoListJSON(data)
	if err != nil {
		t.Fatalf("ParseGoListJSON returned error: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("len(records) = %d, want 2", len(records))
	}
	if records[0].ImportPath != "example.com/project/internal/resource/skill" {
		t.Fatalf("records[0].ImportPath = %q", records[0].ImportPath)
	}
	if records[1].GoFiles[0] != "output.go" {
		t.Fatalf("records[1].GoFiles = %v, want output.go", records[1].GoFiles)
	}
}
