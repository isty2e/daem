package lockfile

import (
	"fmt"

	"github.com/BurntSushi/toml"
)

func validateLockedShapes(metadata *toml.MetaData) error {
	if !metadata.IsDefined("locked", "subject") {
		return nil
	}
	if metadata.Type("locked", "subject") != "ArrayHash" {
		return fmt.Errorf("locked.subject must use [[locked.subject]] array tables")
	}
	return nil
}
