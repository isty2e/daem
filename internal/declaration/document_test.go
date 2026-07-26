package declaration

import (
	"bytes"
	"testing"
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

func TestReplaceDocumentAssignmentLinePreservesIndentAndLineEnding(t *testing.T) {
	block := "[[skill]]\r\n\t targets = [\"codex\", \"claude-code\"]\r\nname = \"alpha\"\r\n"

	updated, ok := ReplaceDocumentAssignmentLine(block, "targets", "[\"codex\"]")
	if !ok {
		t.Fatalf("ReplaceDocumentAssignmentLine() ok = false, want true")
	}
	want := "[[skill]]\r\n\t targets = [\"codex\"]\r\nname = \"alpha\"\r\n"
	if updated != want {
		t.Fatalf("updated block = %q, want %q", updated, want)
	}
}

func TestReplaceDocumentAssignmentLineReportsMissingKey(t *testing.T) {
	block := "[[skill]]\nname = \"alpha\"\n"

	updated, ok := ReplaceDocumentAssignmentLine(block, "targets", "[\"codex\"]")
	if ok {
		t.Fatalf("ReplaceDocumentAssignmentLine() ok = true, want false")
	}
	if updated != block {
		t.Fatalf("updated block = %q, want original %q", updated, block)
	}
}
