package nativeoperation

import (
	"github.com/isty2e/daem/internal/effect/execute"
	"github.com/isty2e/daem/internal/effect/execute/registrysettlement"
)

type ApplyResult struct {
	execute.ApplyResult
	nativeSettlement *registrysettlement.PersistenceSnapshot
}

func (result ApplyResult) NativeSettlement() (registrysettlement.PersistenceSnapshot, bool) {
	if result.nativeSettlement == nil {
		return registrysettlement.PersistenceSnapshot{}, false
	}
	return *result.nativeSettlement, true
}
