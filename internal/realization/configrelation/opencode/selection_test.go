package opencode

import (
	"errors"
	"testing"
)

func TestSelectNameUsesOpenCodePrecedence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		kind     ConfigKind
		existing map[string]bool
		want     string
	}{
		"server json wins": {
			kind:     ConfigServer,
			existing: map[string]bool{"opencode.json": true, "opencode.jsonc": true},
			want:     "opencode.json",
		},
		"server jsonc fallback": {
			kind:     ConfigServer,
			existing: map[string]bool{"opencode.jsonc": true},
			want:     "opencode.jsonc",
		},
		"server default": {
			kind: ConfigServer,
			want: "opencode.json",
		},
		"tui json wins": {
			kind:     ConfigTUI,
			existing: map[string]bool{"tui.json": true, "tui.jsonc": true},
			want:     "tui.json",
		},
		"tui jsonc fallback": {
			kind:     ConfigTUI,
			existing: map[string]bool{"tui.jsonc": true},
			want:     "tui.jsonc",
		},
		"tui default": {
			kind: ConfigTUI,
			want: "tui.json",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := SelectName(test.kind, func(name string) (bool, error) {
				return test.existing[name], nil
			})
			if err != nil {
				t.Fatalf("SelectName: %v", err)
			}
			if got != test.want {
				t.Fatalf("SelectName = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSelectNamePropagatesExistenceFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("stat failed")
	_, err := SelectName(ConfigServer, func(string) (bool, error) {
		return false, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("SelectName error = %v, want %v", err, sentinel)
	}
}
