package declaration

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

type DocumentRange struct {
	Start int
	End   int
}

type DocumentChange struct {
	target      DocumentRange
	replacement []byte
}

func NewDocumentReplacement(target DocumentRange, replacement []byte) DocumentChange {
	return DocumentChange{
		target:      target,
		replacement: append([]byte{}, replacement...),
	}
}

func NewDocumentRemoval(target DocumentRange) DocumentChange {
	return DocumentChange{target: target}
}

type DocumentChangeSet struct {
	changes []DocumentChange
}

func NewDocumentChangeSet(changes ...DocumentChange) DocumentChangeSet {
	copied := make([]DocumentChange, 0, len(changes))
	for _, change := range changes {
		copied = append(copied, DocumentChange{
			target:      change.target,
			replacement: append([]byte{}, change.replacement...),
		})
	}
	return DocumentChangeSet{changes: copied}
}

func (set DocumentChangeSet) Apply(original []byte) ([]byte, error) {
	changes := make([]DocumentChange, len(set.changes))
	copy(changes, set.changes)
	sort.SliceStable(changes, func(left int, right int) bool {
		return changes[left].target.Start < changes[right].target.Start
	})
	for index, change := range changes {
		if change.target.Start < 0 || change.target.End < change.target.Start || change.target.End > len(original) {
			return nil, fmt.Errorf("invalid document change range [%d:%d] for content length %d", change.target.Start, change.target.End, len(original))
		}
		if index > 0 && change.target.Start < changes[index-1].target.End {
			return nil, fmt.Errorf(
				"overlapping document change ranges [%d:%d] and [%d:%d]",
				changes[index-1].target.Start, changes[index-1].target.End,
				change.target.Start, change.target.End,
			)
		}
	}
	output := append([]byte{}, original...)
	for index := len(changes) - 1; index >= 0; index-- {
		change := changes[index]
		output = replaceRange(output, change.target, change.replacement)
	}
	return output, nil
}

func ScanDocumentRanges(content []byte, startsBlock func(string) bool, endsBlock func(string) bool) []DocumentRange {
	lines := bytes.SplitAfter(content, []byte("\n"))
	ranges := make([]DocumentRange, 0)
	offset := 0
	activeStart := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(string(line))
		if activeStart >= 0 && endsBlock(trimmed) {
			ranges = append(ranges, DocumentRange{Start: activeStart, End: offset})
			activeStart = -1
		}
		if startsBlock(trimmed) {
			activeStart = offset
		}
		offset += len(line)
	}
	if activeStart >= 0 {
		ranges = append(ranges, DocumentRange{Start: activeStart, End: len(content)})
	}
	return ranges
}

func AppendDocumentBlock(original []byte, block string) []byte {
	var output bytes.Buffer
	output.Write(original)
	if len(original) != 0 && !bytes.HasSuffix(original, []byte("\n")) {
		output.WriteByte('\n')
	}
	if len(original) != 0 {
		output.WriteByte('\n')
	}
	output.WriteString(block)
	return output.Bytes()
}

func ReplaceDocumentRange(original []byte, target DocumentRange, replacement []byte) []byte {
	content, err := NewDocumentChangeSet(NewDocumentReplacement(target, replacement)).Apply(original)
	if err != nil {
		panic(err)
	}
	return content
}

func RemoveDocumentRange(content []byte, target DocumentRange) []byte {
	output, err := NewDocumentChangeSet(NewDocumentRemoval(target)).Apply(content)
	if err != nil {
		panic(err)
	}
	return output
}

func ReplaceDocumentAssignmentLine(block string, key string, renderedValue string) (string, bool) {
	lines := strings.SplitAfter(block, "\n")
	for index, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), key+" =") {
			continue
		}
		indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		lineEnd := ""
		if strings.HasSuffix(line, "\r\n") {
			lineEnd = "\r\n"
		} else if strings.HasSuffix(line, "\n") {
			lineEnd = "\n"
		}
		lines[index] = indent + key + " = " + renderedValue + lineEnd
		return strings.Join(lines, ""), true
	}
	return block, false
}

func replaceRange(original []byte, target DocumentRange, replacement []byte) []byte {
	content := append([]byte{}, original[:target.Start]...)
	content = append(content, replacement...)
	content = append(content, original[target.End:]...)
	return content
}
