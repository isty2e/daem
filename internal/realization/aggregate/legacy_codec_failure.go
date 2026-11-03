package aggregate

import (
	"strings"

	"github.com/isty2e/daem/internal/output"
)

// NewCodecFailureFromLegacy retains the old token grammar. Its empty path is
// an unscoped diagnostic, not whole-path mutation authority.
func NewCodecFailureFromLegacy(reason CodecFailureReason, contentPath ContentPath) (*CodecFailure, error) {
	if err := reason.validate(); err != nil {
		return nil, err
	}
	location := output.KeyPath{}
	if contentPath != "" {
		if _, err := ParseContentPath(string(contentPath)); err != nil {
			return nil, err
		}
		var err error
		location, err = output.NewKeyPath(strings.Split(string(contentPath)[1:], "/")...)
		if err != nil {
			return nil, err
		}
	}
	return NewCodecFailure(reason, location)
}
