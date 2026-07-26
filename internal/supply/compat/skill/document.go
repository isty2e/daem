package skillcompat

import (
	"context"
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/supply/artifact/access"
)

// MaximumSkillDocumentBytes is the raw byte budget shared by compatibility
// parsing and deterministic skill repair.
const MaximumSkillDocumentBytes int64 = 1 << 20

// ErrSkillDocumentTooLarge classifies a skill compatibility document that
// exceeds the family-owned byte budget.
var ErrSkillDocumentTooLarge = errors.New("skill document exceeds supported size")

// SkillDocumentLimitError reports the first raw byte count known to exceed the
// skill compatibility document budget.
type SkillDocumentLimitError struct {
	limit    int64
	observed int64
}

func (err *SkillDocumentLimitError) Error() string {
	if err == nil {
		return ErrSkillDocumentTooLarge.Error()
	}
	return fmt.Sprintf(
		"%s: observed=%d limit=%d",
		ErrSkillDocumentTooLarge,
		err.observed,
		err.limit,
	)
}

func (err *SkillDocumentLimitError) Unwrap() error {
	return ErrSkillDocumentTooLarge
}

// Limit returns the maximum admitted raw skill document byte count.
func (err *SkillDocumentLimitError) Limit() int64 {
	if err == nil {
		return 0
	}
	return err.limit
}

// Observed returns the first raw byte count known to exceed the limit.
func (err *SkillDocumentLimitError) Observed() int64 {
	if err == nil {
		return 0
	}
	return err.observed
}

// ReadSkillDocument reads one caller-identified skill compatibility document
// without parsing or truncating it.
func ReadSkillDocument(
	ctx context.Context,
	view access.View,
	relativePath string,
) (access.FileContent, error) {
	if relativePath != "SKILL.md" && relativePath != "skill.md" {
		return access.FileContent{}, fmt.Errorf(
			"skill document path must be SKILL.md or skill.md",
		)
	}
	content, err := view.ReadFile(ctx, relativePath, MaximumSkillDocumentBytes)
	if err != nil {
		var limitErr *access.LimitError
		if errors.As(err, &limitErr) {
			return access.FileContent{}, newSkillDocumentLimitError(limitErr.Observed())
		}
		return access.FileContent{}, err
	}
	return content, nil
}

// CheckSkillDocumentSize rejects a complete in-memory document above the same
// budget used by bounded reads.
func CheckSkillDocumentSize(size int64) error {
	if size < 0 {
		return fmt.Errorf("skill document size must not be negative")
	}
	if size > MaximumSkillDocumentBytes {
		return newSkillDocumentLimitError(size)
	}
	return nil
}

func newSkillDocumentLimitError(observed int64) *SkillDocumentLimitError {
	return &SkillDocumentLimitError{
		limit:    MaximumSkillDocumentBytes,
		observed: observed,
	}
}
