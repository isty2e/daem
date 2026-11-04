package execute

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
)

func (key ownershipOutputKey) scopeLabel() string {
	if key.footprint.Kind() == output.FootprintPath {
		return "whole path"
	}
	if path, present := key.footprint.Key(); present {
		return fmt.Sprintf("key %q", path.String())
	}
	return "invalid footprint"
}
