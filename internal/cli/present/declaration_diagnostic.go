package clipresent

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/isty2e/daem/internal/declarationartifact"
)

func DeclarationReplacementGuidance(err error) (Guidance, bool) {
	var replacement declarationartifact.ReplacementError
	if !errors.As(err, &replacement) || (!errors.Is(err, fs.ErrPermission) && !errors.Is(err, syscall.EROFS)) {
		return Guidance{}, false
	}
	return InstructionGuidance("the actual declaration target cannot be replaced; check its ownership and parent directory permissions, or edit its generating configuration if it is in a read-only store; daem preserves the selected link"), true
}

func DeclarationRecoveryGuidance(err error) (Guidance, bool) {
	var linked declarationartifact.LinkedRecoveryError
	if !errors.As(err, &linked) {
		return Guidance{}, false
	}
	return InstructionGuidance("if the manifest or lockfile links changed after interruption, restore their original targets, then retry the exact interrupted write with the same --manifest and selection options; preserve the metadata marker"), true
}
