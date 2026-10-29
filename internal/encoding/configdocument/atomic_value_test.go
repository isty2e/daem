package configdocument_test

import (
	"testing"

	"github.com/isty2e/daem/internal/encoding/configdocument"
)

func TestAtomicValueAndFragmentLeavesUseOneStructuralRule(t *testing.T) {
	for _, test := range []struct {
		value  string
		atomic bool
	}{
		{"null", true},
		{"true", true},
		{"7", true},
		{`"text"`, true},
		{"[]", true},
		{`[{"nested":true}]`, true},
		{"{}", true},
		{`{"nested":true}`, false},
	} {
		document := parse(t, configdocument.JSON, `{"v":`+test.value+`}`)
		value := lookup(t, document, "/v")
		if value.IsAtomic() != test.atomic {
			t.Fatalf("atomic boundary changed for %s", test.value)
		}
		leaves, err := document.Leaves(t.Context(), testLimits.MaximumBytes)
		if err != nil || len(leaves) != 1 {
			t.Fatalf("fragment did not retain its structural leaf: %v", err)
		}
		want := "/v/nested"
		if test.atomic {
			want = "/v"
		}
		if leaves[0].Path != path(t, want) || !leaves[0].Value.IsAtomic() {
			t.Fatalf("fragment expansion disagrees with atomic value classification: %s", test.value)
		}
	}
	if (configdocument.Value{}).IsAtomic() {
		t.Fatal("invalid zero value became a supported atomic value")
	}
	leaves, err := parse(t, configdocument.JSON, `{}`).Leaves(t.Context(), 0)
	if err != nil || len(leaves) != 0 {
		t.Fatal("empty root became a managed leaf")
	}
}
