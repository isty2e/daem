package journal

import "fmt"

// BindRecoveryOperation changes only context-bound readers of the same reserved
// journal retirement. Its record, roots, allocation and single-use state remain.
func (retiring *NativeJournalRetirement) BindRecoveryOperation(operation *NativeRecoveryOperation) error {
	if retiring == nil || retiring.prepared == nil || operation == nil || operation.closed {
		return fmt.Errorf("live native retirement and recovery operation are required")
	}
	prepared := retiring.prepared
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed || prepared.consumed {
		return fmt.Errorf("native retirement continuation is no longer available")
	}
	if retiring.capture != operation.plan.capture || !retiring.active.equal(operation.plan.active) || retiring.budget != operation.profile.Budget {
		return fmt.Errorf("native retirement reader belongs to another operation")
	}
	prepared.stateCodec = operation.profile.StateCodec
	prepared.journalDecoder = decodeNativeInventoryJournal
	if operation.inventoryProfile.decoder != nil {
		prepared.journalDecoder = operation.inventoryProfile.decoder
	}
	return nil
}

func (operation *NativeRecoveryOperation) UsesCapturedRemovalReservations() bool {
	return operation != nil && !operation.closed && operation.capturedRemovalReservations
}
