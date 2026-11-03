package pi

import (
	"bytes"
	"fmt"
)

// PackagePermutation retains an exact row permutation, not package membership
// or managed/foreign ordering authority. Other root fields are not compared.
type PackagePermutation struct {
	before   Document
	expected Document
}

func NewPackagePermutation(before, expected Document) (PackagePermutation, error) {
	if !before.HasPackages() || !expected.HasPackages() {
		return PackagePermutation{}, fmt.Errorf("Pi package permutation requires parsed present arrays")
	}
	if len(before.spans) != len(expected.spans) {
		return PackagePermutation{}, fmt.Errorf("Pi package permutation changes row cardinality")
	}

	byValue := make(map[string][]int, len(before.spans))
	for index, span := range before.spans {
		value := string(before.content[span.start:span.end])
		byValue[value] = append(byValue[value], index)
	}

	order := make([]int, len(expected.spans))
	for index, span := range expected.spans {
		value := string(expected.content[span.start:span.end])
		positions := byValue[value]
		if len(positions) == 0 {
			return PackagePermutation{}, fmt.Errorf("Pi package permutation changes a literal row value")
		}
		order[index] = positions[0]
		byValue[value] = positions[1:]
	}

	candidate, _, err := before.PermutePackageRows(order)
	if err != nil {
		return PackagePermutation{}, err
	}

	permuted, err := Parse(candidate)
	if err != nil {
		return PackagePermutation{}, err
	}
	if !permuted.PackageArrayEqual(expected) {
		return PackagePermutation{}, fmt.Errorf("Pi package permutation changes array slot syntax")
	}

	return PackagePermutation{before: before, expected: expected}, nil
}

func (permutation PackagePermutation) MatchesBefore(document Document) bool {
	return document.PackageArrayEqual(permutation.before)
}

func (permutation PackagePermutation) MatchesExpected(document Document) bool {
	return document.PackageArrayEqual(permutation.expected)
}

// Restore reverses only the recorded array in matching current evidence.
// A changed foreign row or array slot is interference, not permission to reset it.
func (permutation PackagePermutation) Restore(document Document) ([]byte, bool, error) {
	if !permutation.before.HasPackages() || !permutation.expected.HasPackages() {
		return nil, false, fmt.Errorf("Pi package permutation is not initialized")
	}
	if permutation.MatchesBefore(document) {
		return bytes.Clone(document.content), false, nil
	}
	if !permutation.MatchesExpected(document) {
		return nil, false, fmt.Errorf("Pi package array changed outside the recorded permutation")
	}

	before := permutation.before.content[permutation.before.packageSpan.start:permutation.before.packageSpan.end]
	if document.packageSpan.end-document.packageSpan.start != len(before) {
		return nil, false, fmt.Errorf("Pi package permutation array length differs")
	}

	candidate := bytes.Clone(document.content)
	copy(candidate[document.packageSpan.start:document.packageSpan.end], before)

	restored, err := Parse(candidate)
	if err != nil {
		return nil, false, err
	}
	if !restored.PackageArrayEqual(permutation.before) {
		return nil, false, fmt.Errorf("Pi package permutation restoration changed its baseline")
	}

	return candidate, !bytes.Equal(candidate, document.content), nil
}
