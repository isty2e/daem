package declaration

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestScanDocumentRangesKeepsByteStableBlockRanges(t *testing.T) {
	content := []byte("# header\r\n[[item]]\r\nname = \"one\"\r\n[item.detail]\r\nflag = true\r\n[[other]]\r\nname = \"two\"\r\n")

	ranges := ScanDocumentRanges(
		content,
		func(trimmed string) bool { return trimmed == "[[item]]" },
		func(trimmed string) bool {
			if len(trimmed) == 0 || trimmed[0] != '[' {
				return false
			}
			return trimmed != "[item.detail]"
		},
	)

	if len(ranges) != 1 {
		t.Fatalf("ScanDocumentRanges() returned %d ranges, want 1", len(ranges))
	}
	got := string(content[ranges[0].Start:ranges[0].End])
	want := "[[item]]\r\nname = \"one\"\r\n[item.detail]\r\nflag = true\r\n"
	if got != want {
		t.Fatalf("scanned block = %q, want %q", got, want)
	}
}

func TestAppendDocumentBlockSeparatesExistingContent(t *testing.T) {
	got := AppendDocumentBlock([]byte("targets = [\"codex\"]"), "[[skill]]\nname = \"alpha\"\n")
	want := []byte("targets = [\"codex\"]\n\n[[skill]]\nname = \"alpha\"\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendDocumentBlock() = %q, want %q", got, want)
	}

	got = AppendDocumentBlock(nil, "[[skill]]\nname = \"alpha\"\n")
	want = []byte("[[skill]]\nname = \"alpha\"\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendDocumentBlock(empty) = %q, want %q", got, want)
	}
}

func TestReplaceAndRemoveRangePatchOnlySelectedBytes(t *testing.T) {
	content := []byte("alpha\nbeta\ncharlie\n")

	replaced := ReplaceDocumentRange(content, DocumentRange{Start: 6, End: 11}, []byte("BETA\n"))
	if string(replaced) != "alpha\nBETA\ncharlie\n" {
		t.Fatalf("ReplaceDocumentRange() = %q", replaced)
	}
	if string(content) != "alpha\nbeta\ncharlie\n" {
		t.Fatalf("ReplaceDocumentRange mutated original content: %q", content)
	}

	removed := RemoveDocumentRange(content, DocumentRange{Start: 6, End: 11})
	if string(removed) != "alpha\ncharlie\n" {
		t.Fatalf("RemoveDocumentRange() = %q", removed)
	}
}

func TestDocumentChangeSetAppliesMultipleChangesDeterministically(t *testing.T) {
	content := []byte("alpha\nbeta\ncharlie\n")
	set := NewDocumentChangeSet(
		NewDocumentReplacement(DocumentRange{Start: 6, End: 11}, []byte("BETA\n")),
		NewDocumentRemoval(DocumentRange{Start: 0, End: 6}),
	)

	updated, err := set.Apply(content)
	if err != nil {
		t.Fatalf("DocumentChangeSet.Apply() error = %v", err)
	}
	if string(updated) != "BETA\ncharlie\n" {
		t.Fatalf("DocumentChangeSet.Apply() = %q", updated)
	}
	if string(content) != "alpha\nbeta\ncharlie\n" {
		t.Fatalf("DocumentChangeSet.Apply mutated original content: %q", content)
	}
}

func TestDocumentChangeSetRejectsOverlappingRanges(t *testing.T) {
	set := NewDocumentChangeSet(
		NewDocumentRemoval(DocumentRange{Start: 0, End: 6}),
		NewDocumentReplacement(DocumentRange{Start: 3, End: 8}, []byte("bad")),
	)

	_, err := set.Apply([]byte("alpha\nbeta\n"))
	if err == nil {
		t.Fatalf("DocumentChangeSet.Apply() error = nil, want overlapping range error")
	}
}

func TestDocumentChangeSetRejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		name   string
		target DocumentRange
	}{
		{name: "negative start", target: DocumentRange{Start: -1, End: 0}},
		{name: "inverted", target: DocumentRange{Start: 2, End: 1}},
		{name: "past end", target: DocumentRange{Start: 0, End: 4}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			set := NewDocumentChangeSet(NewDocumentRemoval(test.target))
			if _, err := set.Apply([]byte("abc")); err == nil {
				t.Fatalf("DocumentChangeSet.Apply(%+v) error = nil, want invalid range error", test.target)
			}
		})
	}
}

func TestDocumentChangeSetKeepsAdjacentChangesAndNoOpDeterministic(t *testing.T) {
	replacement := []byte("A")
	set := NewDocumentChangeSet(
		NewDocumentReplacement(DocumentRange{Start: 0, End: 1}, replacement),
		NewDocumentReplacement(DocumentRange{Start: 1, End: 2}, []byte("B")),
	)
	replacement[0] = 'X'

	updated, err := set.Apply([]byte("abc"))
	if err != nil {
		t.Fatalf("DocumentChangeSet.Apply() error = %v", err)
	}
	if string(updated) != "ABc" {
		t.Fatalf("DocumentChangeSet.Apply() = %q, want adjacent changes in stable order", updated)
	}

	original := []byte("unchanged")
	unchanged, err := NewDocumentChangeSet().Apply(original)
	if err != nil {
		t.Fatalf("empty DocumentChangeSet.Apply() error = %v", err)
	}
	unchanged[0] = 'U'
	if string(original) != "unchanged" {
		t.Fatalf("empty DocumentChangeSet.Apply() aliased original = %q", original)
	}
}

func TestReplaceRootAssignmentPreservesIndentAndLineEnding(t *testing.T) {
	block := "[[skill]]\r\n\t targets = [\"codex\", \"claude-code\"]\r\nname = \"alpha\"\r\n"

	updated, ok, err := ReplaceRootAssignment(block, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("ReplaceRootAssignment() error = %v", err)
	}
	if !ok {
		t.Fatalf("ReplaceRootAssignment() ok = false, want true")
	}
	want := "[[skill]]\r\n\t targets = [\"codex\"]\r\nname = \"alpha\"\r\n"
	if updated != want {
		t.Fatalf("updated block = %q, want %q", updated, want)
	}
}

func TestReplaceRootAssignmentReportsMissingKey(t *testing.T) {
	block := "[[skill]]\nname = \"alpha\"\n"

	updated, ok, err := ReplaceRootAssignment(block, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("ReplaceRootAssignment() error = %v", err)
	}
	if ok {
		t.Fatalf("ReplaceRootAssignment() ok = true, want false")
	}
	if updated != block {
		t.Fatalf("updated block = %q, want original %q", updated, block)
	}
}

func TestReplaceRootAssignmentCoversCompactMultilineAndComments(t *testing.T) {
	tests := []struct {
		name  string
		block string
		want  string
	}{
		{
			name:  "compact no-space equals",
			block: "[[skill]]\nname = \"alpha\"\ntargets=[\"codex\"]\n",
			want:  "[[skill]]\nname = \"alpha\"\ntargets = [\"claude-code\"]\n",
		},
		{
			name:  "multiline array",
			block: "[[skill]]\nname = \"alpha\"\ntargets = [\n  \"codex\",\n  \"pi\",\n]\nscope = \"project\"\n",
			want:  "[[skill]]\nname = \"alpha\"\ntargets = [\"claude-code\"]\nscope = \"project\"\n",
		},
		{
			name:  "inline comment",
			block: "[[skill]]\nname = \"alpha\"\ntargets = [\"codex\"] # keep\nscope = \"project\"\n",
			want:  "[[skill]]\nname = \"alpha\"\ntargets = [\"claude-code\"] # keep\nscope = \"project\"\n",
		},
		{
			name:  "quoted key",
			block: "[[skill]]\nname = \"alpha\"\n\"targets\" = [\"codex\"]\n",
			want:  "[[skill]]\nname = \"alpha\"\ntargets = [\"claude-code\"]\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updated, ok, err := ReplaceRootAssignment(test.block, "targets", "[\"claude-code\"]")
			if err != nil {
				t.Fatalf("ReplaceRootAssignment() error = %v", err)
			}
			if !ok {
				t.Fatal("ReplaceRootAssignment() ok = false, want true")
			}
			if updated != test.want {
				t.Fatalf("updated block = %q, want %q", updated, test.want)
			}
		})
	}
}

func TestReplaceRootAssignmentIgnoresNestedTableKeys(t *testing.T) {
	block := "[[hook]]\nname = \"lint\"\ncommand = \"run\"\n\n[[hook.target_override]]\ntarget = \"codex\"\n"
	updated, ok, err := ReplaceRootAssignment(block, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("ReplaceRootAssignment() error = %v", err)
	}
	if ok {
		t.Fatalf("replaced nested table key: %q", updated)
	}
}

func TestReplaceRootAssignmentRejectsIncompleteValue(t *testing.T) {
	block := "[[skill]]\nname = \"alpha\"\ntargets = [\n"
	_, ok, err := ReplaceRootAssignment(block, "targets", "[\"codex\"]")
	if err == nil || ok {
		t.Fatalf("ReplaceRootAssignment() = (%v, %v), want malformed error", ok, err)
	}
}

func TestRemoveRootAssignmentRemovesInheritedOverrideLine(t *testing.T) {
	block := "[[hook]]\nname = \"lint\"\ntargets = [\"codex\"] # keep\ncommand = \"run\"\n"
	updated, ok, err := RemoveRootAssignment(block, "targets")
	if err != nil {
		t.Fatalf("RemoveRootAssignment() error = %v", err)
	}
	if !ok {
		t.Fatal("RemoveRootAssignment() ok = false, want true")
	}
	want := "[[hook]]\nname = \"lint\"\ncommand = \"run\"\n"
	if updated != want {
		t.Fatalf("updated block = %q, want %q", updated, want)
	}
}

func TestReplaceRootAssignmentSkipsDottedKeysAndRewritesTargets(t *testing.T) {
	block := "[[skill]]\nname = \"oracle\"\ntarget.codex.install_to = \"skills/oracle\"\ntargets = [\"codex\", \"claude-code\"]\n"
	updated, ok, err := ReplaceRootAssignment(block, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("ReplaceRootAssignment() error = %v", err)
	}
	if !ok {
		t.Fatal("ReplaceRootAssignment() ok = false, want true")
	}
	want := "[[skill]]\nname = \"oracle\"\ntarget.codex.install_to = \"skills/oracle\"\ntargets = [\"codex\"]\n"
	if updated != want {
		t.Fatalf("updated block = %q, want %q", updated, want)
	}
}

func TestReplaceRootAssignmentIgnoresTableLikeMultilineString(t *testing.T) {
	block := "[[skill]]\nname = \"oracle\"\nnote = \"\"\"\n[skill.codex]\n\"\"\"\ntargets = [\"codex\", \"claude-code\"]\n"
	updated, ok, err := ReplaceRootAssignment(block, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("ReplaceRootAssignment() error = %v", err)
	}
	if !ok {
		t.Fatal("ReplaceRootAssignment() ok = false, want true")
	}
	if !strings.Contains(updated, `targets = ["codex"]`) || strings.Contains(updated, `targets = ["codex", "claude-code"]`) {
		t.Fatalf("updated block = %q", updated)
	}
	if !strings.Contains(updated, "[skill.codex]") {
		t.Fatalf("multiline string content was rewritten: %q", updated)
	}
}

func TestReplaceRootAssignmentLinearOnLongStringValue(t *testing.T) {
	long := strings.Repeat("a", 32*1024)
	block := "[[skill]]\nname = \"" + long + "\"\ntargets = [\"codex\"]\n"
	start := time.Now()
	updated, ok, err := ReplaceRootAssignment(block, "targets", "[\"claude-code\"]")
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ReplaceRootAssignment() error = %v", err)
	}
	if !ok {
		t.Fatal("ReplaceRootAssignment() ok = false, want true")
	}
	if !strings.Contains(updated, `targets = ["claude-code"]`) {
		t.Fatalf("updated block missing rewritten targets")
	}
	if elapsed > 250*time.Millisecond {
		t.Fatalf("ReplaceRootAssignment took %s on 32KiB string, want linear scan", elapsed)
	}
}

func TestScanDocumentRangesIgnoresTableHeadersInsideMultilineStrings(t *testing.T) {
	content := []byte("[[item]]\nnote = \"\"\"\n[[other]]\n\"\"\"\nname = \"one\"\n[[other]]\nname = \"two\"\n")
	ranges := ScanDocumentRanges(
		content,
		func(trimmed string) bool { return trimmed == "[[item]]" || trimmed == "[[other]]" },
		func(trimmed string) bool {
			header, ok := ParseTableHeader(trimmed)
			return ok && header.Array
		},
	)
	if len(ranges) != 2 {
		t.Fatalf("ScanDocumentRanges() returned %d ranges, want 2", len(ranges))
	}
	first := string(content[ranges[0].Start:ranges[0].End])
	if !strings.Contains(first, "[[other]]") || !strings.Contains(first, `name = "one"`) {
		t.Fatalf("multiline lookalike truncated the first block: %q", first)
	}
	second := string(content[ranges[1].Start:ranges[1].End])
	if !strings.Contains(second, `name = "two"`) {
		t.Fatalf("second block = %q", second)
	}
}

func TestReplaceRootAssignmentAcceptsFourAndFiveQuoteMultilineEndings(t *testing.T) {
	tests := []struct {
		name  string
		block string
	}{
		{
			name:  "basic four",
			block: "[[hook]]\ncommand = \"\"\"echo hi\"\"\"\"\ntargets = [\"codex\", \"claude-code\"]\n",
		},
		{
			name:  "basic five",
			block: "[[hook]]\ncommand = \"\"\"echo hi\"\"\"\"\"\ntargets = [\"codex\", \"claude-code\"]\n",
		},
		{
			name:  "literal four",
			block: "[[hook]]\ncommand = '''echo hi''''\ntargets = [\"codex\", \"claude-code\"]\n",
		},
		{
			name:  "literal five",
			block: "[[hook]]\ncommand = '''echo hi'''''\ntargets = [\"codex\", \"claude-code\"]\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updated, ok, err := ReplaceRootAssignment(test.block, "targets", "[\"codex\"]")
			if err != nil {
				t.Fatalf("ReplaceRootAssignment() error = %v", err)
			}
			if !ok {
				t.Fatal("ReplaceRootAssignment() ok = false, want true")
			}
			if !strings.Contains(updated, `targets = ["codex"]`) || strings.Contains(updated, `targets = ["codex", "claude-code"]`) {
				t.Fatalf("updated block = %q", updated)
			}
		})
	}
}

func TestInsertRootAssignmentSeparatesNoFinalNewlineAndPreservesCRLF(t *testing.T) {
	block := "[[hook]]\nname = \"lint\"\nscope = \"project\""
	updated, err := InsertRootAssignment(block, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("InsertRootAssignment() error = %v", err)
	}
	want := "[[hook]]\nname = \"lint\"\nscope = \"project\"\ntargets = [\"codex\"]\n"
	if updated != want {
		t.Fatalf("updated block = %q, want %q", updated, want)
	}

	crlf := "[[hook]]\r\nname = \"lint\"\r\n\r\n[[hook.target_override]]\r\ntarget = \"codex\"\r\n"
	inserted, err := InsertRootAssignment(crlf, "targets", "[\"codex\"]")
	if err != nil {
		t.Fatalf("InsertRootAssignment(crlf) error = %v", err)
	}
	if !strings.Contains(inserted, "\r\ntargets = [\"codex\"]\r\n[[hook.target_override]]") {
		t.Fatalf("CRLF insert = %q", inserted)
	}
	if strings.Contains(strings.ReplaceAll(inserted, "\r\n", ""), "\n") {
		t.Fatalf("CRLF insert mixed line endings: %q", inserted)
	}
}
