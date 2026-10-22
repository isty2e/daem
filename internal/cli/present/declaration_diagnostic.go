package clipresent

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/effect/fileset"
)

func DeclarationReplacementGuidance(err error) (Guidance, bool) {
	var replacement declarationartifact.ReplacementError
	if !errors.As(err, &replacement) || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.EROFS)) {
		return Guidance{}, false
	}
	return InstructionGuidance("the actual declaration target cannot be replaced; check its ownership and parent directory permissions, or edit its generating configuration if it is in a read-only store; daem preserves the selected link"), true
}

func DeclarationRecoveryGuidance(err error) (Guidance, bool) {
	var authority fileset.RecoveryAuthorityError
	if !errors.As(err, &authority) {
		return Guidance{}, false
	}
	return InstructionGuidance("restore the manifest and lockfile links to their original targets, then retry the exact interrupted write with the same --manifest and selection options; preserve the metadata marker; daem recover does not consume it"), true
}
