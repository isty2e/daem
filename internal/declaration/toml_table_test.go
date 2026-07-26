package declaration

import "testing"

func TestParseTableHeaderHandlesCommentsAndQuotedSegments(t *testing.T) {
	tests := []struct {
		line     string
		segments []string
		array    bool
		ok       bool
	}{
		{line: `[[skill]] # inline comment`, segments: []string{"skill"}, array: true, ok: true},
		{line: `[["skill"]] # quoted array root`, segments: []string{"skill"}, array: true, ok: true},
		{line: `[mcp_server.env."API TOKEN"] # inline comment`, segments: []string{"mcp_server", "env", "API TOKEN"}, ok: true},
		{line: `["skill_group".source]`, segments: []string{"skill_group", "source"}, ok: true},
		{line: `[[hook.target_override]]`, segments: []string{"hook", "target_override"}, array: true, ok: true},
		{line: `[not a header`, ok: false},
		{line: `name = "alpha"`, ok: false},
		{line: `[skill] trailing`, ok: false},
	}

	for _, test := range tests {
		t.Run(test.line, func(t *testing.T) {
			header, ok := ParseTableHeader(test.line)
			if ok != test.ok {
				t.Fatalf("ParseTableHeader(%q) ok = %v, want %v", test.line, ok, test.ok)
			}
			if !ok {
				return
			}
			if header.Array != test.array {
				t.Fatalf("array = %v, want %v", header.Array, test.array)
			}
			if len(header.Segments) != len(test.segments) {
				t.Fatalf("segments = %#v, want %#v", header.Segments, test.segments)
			}
			for index, want := range test.segments {
				if header.Segments[index] != want {
					t.Fatalf("segments = %#v, want %#v", header.Segments, test.segments)
				}
			}
		})
	}
}

func TestStartsTableOutsideRootKeepsNestedRootTables(t *testing.T) {
	tests := []struct {
		line string
		root string
		want bool
	}{
		{line: `name = "alpha"`, root: "skill", want: false},
		{line: `[skill.source]`, root: "skill", want: false},
		{line: `[[skill.target]]`, root: "skill", want: false},
		{line: `[[skill]] # inline comment`, root: "skill", want: true},
		{line: `[[skill_group]]`, root: "skill", want: true},
		{line: `[mcp_server.env."API TOKEN"]`, root: "mcp_server", want: false},
		{line: `[[hook]]`, root: "mcp_server", want: true},
	}

	for _, test := range tests {
		t.Run(test.line+"/"+test.root, func(t *testing.T) {
			if got := StartsTableOutsideRoot(test.line, test.root); got != test.want {
				t.Fatalf("StartsTableOutsideRoot(%q, %q) = %v, want %v", test.line, test.root, got, test.want)
			}
		})
	}
}

func TestStartsArrayTableRootMatchesOnlyRootArrayTables(t *testing.T) {
	tests := []struct {
		line string
		root string
		want bool
	}{
		{line: `[[skill]] # inline comment`, root: "skill", want: true},
		{line: `[["skill"]] # quoted root`, root: "skill", want: true},
		{line: `["skill".source]`, root: "skill", want: false},
		{line: `[[skill.source]]`, root: "skill", want: false},
		{line: `[[skill_group]]`, root: "skill", want: false},
		{line: `name = "alpha"`, root: "skill", want: false},
	}

	for _, test := range tests {
		t.Run(test.line+"/"+test.root, func(t *testing.T) {
			if got := StartsArrayTableRoot(test.line, test.root); got != test.want {
				t.Fatalf("StartsArrayTableRoot(%q, %q) = %v, want %v", test.line, test.root, got, test.want)
			}
		})
	}
}
