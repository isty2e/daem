package jsonstrict

import (
	"errors"
	"testing"
)

func TestValidateRecordAppliesFieldAndNullRulesAtEveryDepth(t *testing.T) {
	for _, content := range []string{
		`{}`, `{"enabled":false,"mode":0,"empty":[],"literal":"null"}`,
		`{"claims":[{"footprint":{"kind":"key","key_path":"/CamelCase/a~1b/日本語"}}]}`,
		`{"key":"Key","list":["CamelCase",false,0,{},[]]}`,
		`{"literal":"{\"User-Key\":null}"}`,
	} {
		if err := ValidateRecord([]byte(content), "record", 8); err != nil {
			t.Fatalf("valid record was refused: %v", err)
		}
	}
	for _, content := range []string{
		`null`, `[]`, `true`, `"record"`,
		`{"value":null}`, `{"values":[null]}`,
		`{"authority":{"key":"before","Key":"after"}}`,
		`{"authority":{"Key":"after","key":"before"}}`,
		`{"claims":[{"footprint":{"Kind":"path"}}]}`,
		`{"claims":[{"footprint":{"kind":"path","key_path":null}}]}`,
		`{"claims":[[{}, {"Key":"after"}]]}`,
	} {
		if err := ValidateRecord([]byte(content), "record", 8); err == nil {
			t.Fatalf("noncanonical nested record was admitted: %s", content)
		}
	}
}

func TestValidateRecordRetainsSharedSyntaxLimitsAndLegacyValueSemantics(t *testing.T) {
	if err := ValidateRecord([]byte(`{"items":[{"key":1,"key":2}]}`), "record", 8); !errors.Is(err, ErrDuplicateObjectKey) {
		t.Fatalf("duplicate-key classification changed: %v", err)
	}
	if err := ValidateRecord([]byte(`{"items":[{"key":1}]}`), "record", 2); !errors.Is(err, ErrMaximumDepthExceeded) {
		t.Fatalf("shared maximum-depth refusal changed: %v", err)
	}
	if err := ValidateRecord([]byte(`{"items":[{"key":1}]}`), "record", 3); err != nil {
		t.Fatalf("inclusive shared maximum-depth boundary changed: %v", err)
	}
	if err := Validate([]byte(`{"User-Key":null,"values":[null,{"日本語":true}]}`), "host document", 8); err != nil {
		t.Fatalf("record admission widened into ordinary JSON semantics: %v", err)
	}
	if _, err := ValidateVersionedObject([]byte(`{"version":16,"optional":null}`), "versioned object", 8); err != nil {
		t.Fatalf("existing version-envelope null semantics changed: %v", err)
	}
}
