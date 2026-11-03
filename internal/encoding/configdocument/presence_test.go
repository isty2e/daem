package configdocument_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestDocumentValuePresence(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		syntax  configdocument.Syntax
		content string
		present bool
	}{
		{"JSON empty root", configdocument.JSON, `{}`, false},
		{"TOML empty root", configdocument.TOML, "", false},
		{"comments only", configdocument.TOML, "# retained\n", false},
		{"empty object value", configdocument.JSON, `{"value":{}}`, true},
		{"empty array value", configdocument.JSON, `{"value":[]}`, true},
		{"empty TOML table", configdocument.TOML, "[value]\n", true},
		{"null value", configdocument.JSON, `{"value":null}`, true},
		{"root array", configdocument.JSON, `[]`, true},
		{"root null", configdocument.JSON, `null`, true},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			document := parse(t, fixture.syntax, fixture.content)
			if document.HasValues() != fixture.present {
				t.Fatalf("value presence = %v, want %v", document.HasValues(), fixture.present)
			}
		})
	}

	absent, err := configdocument.Absent(configdocument.TOML, testLimits)
	if err != nil {
		t.Fatal(err)
	}
	if absent.HasValues() {
		t.Fatal("absent document has values")
	}
}
