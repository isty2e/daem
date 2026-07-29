package lockfile

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

func validateLockedShapes(metadata *toml.MetaData) error {
	if metadata.IsDefined("locked", "subject") &&
		metadata.Type("locked", "subject") != "ArrayHash" {
		return fmt.Errorf("locked.subject must use [[locked.subject]] array tables")
	}
	if metadata.IsDefined("locked", "order_constraint") &&
		metadata.Type("locked", "order_constraint") != "ArrayHash" {
		return fmt.Errorf(
			"locked.order_constraint must use [[locked.order_constraint]] array tables",
		)
	}
	if metadata.IsDefined("locked", "order_constraint", "member") &&
		metadata.Type("locked", "order_constraint", "member") != "ArrayHash" {
		return fmt.Errorf(
			"locked.order_constraint.member must use [[locked.order_constraint.member]] array tables",
		)
	}
	return nil
}
