package clipresent

import (
	"bytes"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestHelpRowPreservesDescriptionAlignment(t *testing.T) {
	labels := []struct {
		name  string
		value string
	}{
		{name: "unlabeled"},
		{name: "short", value: "--manifest <path>"},
		{name: "below_padding_boundary", value: "--" + strings.Repeat("x", 23)},
		{name: "at_padding_boundary", value: "--" + strings.Repeat("x", 24)},
		{name: "above_padding_boundary", value: "--" + strings.Repeat("x", 25)},
		{name: "long", value: "--" + strings.Repeat("x", 28)},
	}
	descriptions := []struct {
		name    string
		text    string
		wrapped bool
	}{
		{name: "fitting", text: "alpha"},
		{name: "wrapped", text: strings.Repeat("alpha beta gamma delta epsilon ", 8), wrapped: true},
	}

	for _, width := range []int{40, 80, 120} {
		for _, label := range labels {
			for _, description := range descriptions {
				t.Run(label.name+"/"+description.name+"/"+strconv.Itoa(width), func(t *testing.T) {
					var output bytes.Buffer
					printHelpRow(&output, helpRow{Label: label.value, Text: description.text}, width)

					column := 2
					if label.value != "" {
						column = max(28, 2+len(label.value)+1)
					}
					firstPrefix := "  " + label.value + strings.Repeat(" ", column-2-len(label.value))
					continuationPrefix := strings.Repeat(" ", column)
					lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
					if description.wrapped != (len(lines) > 1) {
						t.Fatalf("got %d lines, want wrapped=%t:\n%s", len(lines), description.wrapped, output.String())
					}

					var words []string
					for index, line := range lines {
						prefix := continuationPrefix
						if index == 0 {
							prefix = firstPrefix
						}
						if !strings.HasPrefix(line, prefix) {
							t.Fatalf("line %d = %q, want prefix %q", index+1, line, prefix)
						}
						if len(line) <= column || line[column] == ' ' {
							t.Fatalf("line %d = %q, description must start at column %d", index+1, line, column)
						}
						if len(line) > width {
							t.Errorf("line %d has %d bytes, width %d: %q", index+1, len(line), width, line)
						}
						words = append(words, strings.Fields(line[column:])...)
					}
					if expected := strings.Fields(description.text); !slices.Equal(words, expected) {
						t.Fatalf("description words = %q, want %q", words, expected)
					}
				})
			}
		}
	}
}

func TestHelpParagraphWrappingPreservesLayout(t *testing.T) {
	for _, test := range []struct {
		name  string
		line  string
		width int
		want  string
	}{
		{name: "empty", width: 40, want: "\n"},
		{name: "blank", line: "   ", width: 40, want: "   \n"},
		{name: "fitting", line: "  alpha  beta", width: 40, want: "  alpha  beta\n"},
		{
			name:  "wrapped",
			line:  "  alpha beta gamma delta epsilon alpha beta gamma delta epsilon",
			width: 40,
			want:  "  alpha beta gamma delta epsilon alpha\n  beta gamma delta epsilon\n",
		},
		{
			name:  "indivisible_word",
			line:  "  " + strings.Repeat("x", 40) + " alpha",
			width: 40,
			want:  "  " + strings.Repeat("x", 40) + "\n  alpha\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			printWrappedLines(&output, []string{test.line}, test.width)
			if actual := output.String(); actual != test.want {
				t.Fatalf("paragraph output = %q, want %q", actual, test.want)
			}
		})
	}
}
