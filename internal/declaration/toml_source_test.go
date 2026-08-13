package declaration

import (
	"fmt"
	"testing"
)

func TestSourceFromTOMLValueRejectsNormalizedDuplicateKeysDeterministically(t *testing.T) {
	keys := []string{
		"format",
		"git",
		"mode",
		"path",
		"ref",
		"region",
		"s3",
		"version_id",
	}

	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			values := map[string]any{
				key:             "canonical",
				" " + key + " ": "alias",
			}
			want := fmt.Sprintf("duplicate source key %q after normalization", key)
			for attempt := 0; attempt < 100; attempt++ {
				_, err := SourceFromTOMLValue(values)
				if err == nil || err.Error() != want {
					t.Fatalf("attempt %d error = %v, want %q", attempt, err, want)
				}
			}
		})
	}
}

func TestSourceFromTOMLValueRejectsNoncanonicalAndUnknownKeys(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   string
	}{
		{
			name:   "noncanonical",
			values: map[string]any{" path ": "skill"},
			want:   `source key " path " must use canonical spelling "path"`,
		},
		{
			name:   "unknown",
			values: map[string]any{"unknown": "skill"},
			want:   `unknown source key "unknown"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := SourceFromTOMLValue(test.values)
			if err == nil || err.Error() != test.want {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestSourceFromTOMLValueValidatesKeysBeforeValuesDeterministically(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   string
	}{
		{
			name: "duplicate before value type",
			values: map[string]any{
				"path":   42,
				" path ": "skill",
			},
			want: `duplicate source key "path" after normalization`,
		},
		{
			name: "sorted value validation",
			values: map[string]any{
				"path": 42,
				"git":  false,
			},
			want: "source.git: must be a string",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for attempt := 0; attempt < 100; attempt++ {
				_, err := SourceFromTOMLValue(test.values)
				if err == nil || err.Error() != test.want {
					t.Fatalf("attempt %d error = %v, want %q", attempt, err, test.want)
				}
			}
		})
	}
}

func TestSourceFromTOMLValueIgnoresValidTableOrdering(t *testing.T) {
	left, err := SourceFromTOMLValue(map[string]any{
		"git":  "https://example.com/skill.git",
		"ref":  "v1.2.3",
		"mode": "vendor",
	})
	if err != nil {
		t.Fatalf("decode left source: %v", err)
	}
	right, err := SourceFromTOMLValue(map[string]any{
		"mode": "vendor",
		"ref":  "v1.2.3",
		"git":  "https://example.com/skill.git",
	})
	if err != nil {
		t.Fatalf("decode right source: %v", err)
	}
	if left != right {
		t.Fatalf("sources differ: left=%+v right=%+v", left, right)
	}
}
