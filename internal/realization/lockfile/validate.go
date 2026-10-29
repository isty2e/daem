package lockfile

import (
	"fmt"

	"github.com/isty2e/daem/internal/encoding/toml"
)

func validateLockedShapes(metadata *tomlsyntax.Metadata) error {
	if metadata.IsDefined("locked", "subject") &&
		!metadata.IsArrayTable("locked", "subject") {
		return fmt.Errorf("locked.subject must use [[locked.subject]] array tables")
	}
	if metadata.IsDefined("locked", "order_constraint") &&
		!metadata.IsArrayTable("locked", "order_constraint") {
		return fmt.Errorf(
			"locked.order_constraint must use [[locked.order_constraint]] array tables",
		)
	}
	if metadata.IsDefined("locked", "order_constraint", "member") &&
		!metadata.IsArrayTable("locked", "order_constraint", "member") {
		return fmt.Errorf(
			"locked.order_constraint.member must use [[locked.order_constraint.member]] array tables",
		)
	}
	return nil
}
