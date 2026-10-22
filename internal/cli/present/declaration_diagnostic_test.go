package clipresent

import (
	"fmt"
	"syscall"
	"testing"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
)

func TestDeclarationFailureGuidanceUsesTypedCauses(t *testing.T) {
	for _, cause := range []error{syscall.EROFS, syscall.EACCES, syscall.EPERM} {
		err := fmt.Errorf("lock failed: %w", declarationartifact.ReplacementError{Path: "/fixture/actual.lock", Err: cause})
		diagnostic := FailureDiagnosticFromError(err)
		if len(diagnostic.Guidance()) == 0 || diagnostic.Guidance()[0].Kind() != GuidanceInstruction {
			t.Fatalf("permission failure has no manual remedy: %#v", diagnostic)
		}
	}
	err := declarationartifact.ReplacementError{Path: "/fixture/actual.lock", Err: syscall.EIO}
	if _, present := DeclarationReplacementGuidance(err); present {
		t.Fatal("I/O failure was misclassified as permission or readonly")
	}
	guidance, present := DeclarationRecoveryGuidance(fmt.Errorf("write failed: %w", fileset.RecoveryAuthorityError{Path: "/fixture/original.lock"}))
	if !present || guidance.Kind() != GuidanceInstruction || guidance.Message() == "" {
		t.Fatalf("changed-link recovery has no manual retry guidance: %#v", guidance)
	}
	if len(guidance.Argv()) != 0 {
		t.Fatal("metadata marker recovery was projected as a generic command")
	}
}
