package compose

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
)

// VerifyCandidate checks selected codec meaning and protected values/comments.
// A codec's rewrite envelope is not permission to change its foreign members.
func VerifyCandidate(ctx context.Context, original, expected aggregate.Document, limits configdocument.Limits, participants []Participant) error {
	baselines, err := ReadBaselines(ctx, expected, limits, participants)
	if err != nil {
		return err
	}
	projected, err := Restore(ctx, original, limits, baselines)
	if err != nil {
		return err
	}
	actual := projected.Document()
	if actual.Exists() != expected.Exists() {
		return fmt.Errorf("document candidate changes protected content")
	}
	if !actual.Exists() {
		return ctx.Err()
	}

	left, err := parse(ctx, actual, participants[0].syntax, limits)
	if err != nil {
		return err
	}
	right, err := parse(ctx, expected, participants[0].syntax, limits)
	if err != nil {
		return err
	}
	leftRoot, _, err := left.Lookup(output.KeyPath{})
	if err != nil {
		return err
	}
	rightRoot, _, err := right.Lookup(output.KeyPath{})
	if err != nil {
		return err
	}
	leftComments, rightComments := left.Comments(), right.Comments()
	slices.Sort(leftComments)
	slices.Sort(rightComments)
	if !leftRoot.Equal(rightRoot) || !slices.Equal(leftComments, rightComments) {
		return fmt.Errorf("document candidate changes protected content")
	}
	return ctx.Err()
}
