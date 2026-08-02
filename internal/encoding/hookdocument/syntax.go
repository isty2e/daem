// Package hookdocument owns the bounded strict-JSON grammar shared by hook
// host-document import and managed projection.
package hookdocument

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
)

const (
	// MaximumBytes bounds one hook host document at every ingress.
	MaximumBytes int64 = 4 << 20
	// MaximumDepth bounds JSON nesting at every hook host-document ingress.
	MaximumDepth = 64
)

// ErrTooLarge classifies a hook host document beyond its byte budget.
var ErrTooLarge = errors.New("hook host document size limit exceeded")

// Validate requires the byte grammar shared by hook import and managed hook
// projection.
func Validate(content []byte) error {
	if int64(len(content)) > MaximumBytes {
		return fmt.Errorf("%w: maximum=%d bytes", ErrTooLarge, MaximumBytes)
	}
	return jsonstrict.Validate(content, "hook host document", MaximumDepth)
}
