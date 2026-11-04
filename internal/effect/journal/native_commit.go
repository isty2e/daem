package journal

import (
	"fmt"

	"github.com/isty2e/daem/internal/assurance/durable"
)

func requireNativeDocumentCommitWork(before, after durable.Snapshot, documentWork bool) error {
	if err := before.ValidateNativeDocumentCommitTransition(after); err != nil {
		return err
	}
	if before.NativeDocumentCommit() != after.NativeDocumentCommit() && !documentWork {
		return fmt.Errorf("native document commit requires actual document publication work")
	}
	return nil
}
