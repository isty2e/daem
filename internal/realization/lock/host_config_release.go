package lock

import (
	"fmt"

	"github.com/isty2e/daem/internal/desired/entity"
)

// WithoutHostConfig removes one declaration's source and every locked output
// together. Other subjects and order constraints retain their exact values.
func (file File) WithoutHostConfig(id entity.ID) (File, bool, error) {
	if err := id.Validate(); err != nil || id.Kind() != entity.KindHostConfig {
		return File{}, false, fmt.Errorf("lock release requires a host-config entity identity")
	}
	if err := Validate(file); err != nil {
		return File{}, false, err
	}
	var retained []LockedSubjectContract
	removed := false
	for _, contract := range file.Locked.subjects {
		if contract.EntityID() == id {
			removed = true
		} else {
			retained = append(retained, contract)
		}
	}
	if !removed {
		return file, false, nil
	}
	locked, err := NewLockedSection(retained, file.Locked.orderConstraints)
	if err != nil {
		return File{}, false, err
	}
	return File{Version: file.Version, Locked: locked}, true, nil
}
