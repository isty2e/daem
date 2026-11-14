package document

import (
	"context"
	"fmt"
	"slices"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

func (comparison Comparison) RequireCanonicalRecoveryPolicy(ctx context.Context, canonical Comparison) error {
	if ctx == nil {
		return fmt.Errorf("document comparison context is required")
	}
	if err := comparison.Validate(); err != nil {
		return err
	}
	if err := canonical.Validate(); err != nil {
		return err
	}
	if !slices.EqualFunc(comparison.sequences, canonical.sequences, func(left, right configdocument.JSONArrayEdit) bool { return left.Equal(right) }) {
		return fmt.Errorf("prepared native sequence edits differ from the recorded comparison")
	}
	participants := make(map[documentSelectionKey]compose.Participant, len(canonical.participants))
	for _, participant := range canonical.participants {
		selection := participant.Selection()
		participants[documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}] = participant
	}
	areas := make(map[documentSelectionKey][]output.KeyPath, len(participants))
	for _, participant := range comparison.participants {
		if err := ctx.Err(); err != nil {
			return err
		}
		selection := participant.Selection()
		key := documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}
		expected, found := participants[key]
		if !found || participant.Syntax() != expected.Syntax() {
			return fmt.Errorf("prepared participant grammar or codec contract differs from the operation")
		}
		areas[key] = append(areas[key], participant.RewriteAreas()...)
	}
	for _, participant := range canonical.participants {
		if err := ctx.Err(); err != nil {
			return err
		}
		selection := participant.Selection()
		key := documentSelectionKey{document: selection.DocumentAddress(), codec: selection.CodecContractID()}
		if !slices.Equal(output.TopmostKeyPaths(areas[key]), participant.RewriteAreas()) {
			return fmt.Errorf("prepared native rewrite envelope differs from the operation")
		}
	}
	return ctx.Err()
}
