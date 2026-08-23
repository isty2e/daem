//go:build windows

package commit

import (
	"errors"
	"fmt"

	"golang.org/x/sys/windows"
)

var (
	errWindowsNativeUnsupported   = errors.New("Windows storage substrate capability is unsupported")
	errWindowsNativeIndeterminate = errors.New("Windows storage operation outcome is indeterminate")
)

// windowsNativeErrorClass is deliberately narrower than the public commit
// failure algebra. The later adapter combines this phase-aware evidence with
// post-visibility observations before selecting a public FailureKind.
type windowsNativeErrorClass uint8

const (
	windowsNativeErrorUnknown windowsNativeErrorClass = iota
	windowsNativeErrorNotFound
	windowsNativeErrorCollision
	windowsNativeErrorSharing
	windowsNativeErrorUnsupported
	windowsNativeErrorIndeterminate
)

type windowsNativePhase string

const (
	windowsNativePhaseOpen        windowsNativePhase = "open"
	windowsNativePhaseCreate      windowsNativePhase = "create"
	windowsNativePhaseRead        windowsNativePhase = "read"
	windowsNativePhaseWrite       windowsNativePhase = "write"
	windowsNativePhaseEnumerate   windowsNativePhase = "enumerate"
	windowsNativePhaseIdentity    windowsNativePhase = "identity"
	windowsNativePhaseMetadata    windowsNativePhase = "metadata"
	windowsNativePhaseSecurity    windowsNativePhase = "security"
	windowsNativePhaseRename      windowsNativePhase = "rename"
	windowsNativePhaseDisposition windowsNativePhase = "disposition"
	windowsNativePhaseFlush       windowsNativePhase = "flush"
)

type windowsNativeError struct {
	class          windowsNativeErrorClass
	phase          windowsNativePhase
	postVisibility bool
	cause          error
}

func (err *windowsNativeError) Error() string {
	if err == nil {
		return "<nil>"
	}
	message := fmt.Sprintf("Windows storage %s error during %s", windowsNativeClassName(err.class), err.phase)
	if err.postVisibility {
		message += " after possible visibility"
	}
	if err.cause != nil {
		message += ": " + err.cause.Error()
	}
	return message
}

func (err *windowsNativeError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

func (err *windowsNativeError) Is(target error) bool {
	if err == nil {
		return false
	}
	switch {
	case target == errWindowsNativeUnsupported:
		return err.class == windowsNativeErrorUnsupported
	case target == errWindowsNativeIndeterminate:
		return err.class == windowsNativeErrorIndeterminate
	default:
		return false
	}
}

func (err *windowsNativeError) Class() windowsNativeErrorClass {
	if err == nil {
		return windowsNativeErrorUnknown
	}
	return err.class
}

func (err *windowsNativeError) Phase() windowsNativePhase {
	if err == nil {
		return ""
	}
	return err.phase
}

func (err *windowsNativeError) PostVisibility() bool {
	return err != nil && err.postVisibility
}

func windowsNativeClassName(class windowsNativeErrorClass) string {
	switch class {
	case windowsNativeErrorNotFound:
		return "not-found"
	case windowsNativeErrorCollision:
		return "collision"
	case windowsNativeErrorSharing:
		return "sharing"
	case windowsNativeErrorUnsupported:
		return "unsupported"
	case windowsNativeErrorIndeterminate:
		return "indeterminate"
	default:
		return "unknown"
	}
}

func normalizeWindowsNativeError(
	phase windowsNativePhase,
	cause error,
	postVisibility bool,
) error {
	if cause == nil {
		return nil
	}
	var existing *windowsNativeError
	if errors.As(cause, &existing) {
		copy := *existing
		copy.phase = phase
		copy.postVisibility = copy.postVisibility || postVisibility
		return &copy
	}
	class := classifyWindowsNativeError(cause)
	if postVisibility && class == windowsNativeErrorUnknown {
		class = windowsNativeErrorIndeterminate
	}
	return &windowsNativeError{
		class:          class,
		phase:          phase,
		postVisibility: postVisibility,
		cause:          cause,
	}
}

func classifyWindowsNativeError(err error) windowsNativeErrorClass {
	if err == nil {
		return windowsNativeErrorUnknown
	}
	if errors.Is(err, errWindowsNativeUnsupported) {
		return windowsNativeErrorUnsupported
	}
	for current := err; current != nil; {
		if status, ok := current.(windows.NTStatus); ok {
			return classifyWindowsNativeStatus(status)
		}
		type unwrapper interface{ Unwrap() error }
		wrapped, ok := current.(unwrapper)
		if !ok {
			break
		}
		current = wrapped.Unwrap()
	}
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || errors.Is(err, windows.ERROR_PATH_NOT_FOUND) {
		return windowsNativeErrorNotFound
	}
	if errors.Is(err, windows.ERROR_FILE_EXISTS) || errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return windowsNativeErrorCollision
	}
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return windowsNativeErrorSharing
	}
	if errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED) || errors.Is(err, windows.ERROR_CALL_NOT_IMPLEMENTED) ||
		errors.Is(err, windows.ERROR_EAS_NOT_SUPPORTED) {
		return windowsNativeErrorUnsupported
	}
	return windowsNativeErrorUnknown
}

func classifyWindowsNativeStatus(status windows.NTStatus) windowsNativeErrorClass {
	switch status {
	case windows.STATUS_NO_SUCH_FILE, windows.STATUS_OBJECT_NAME_NOT_FOUND, windows.STATUS_OBJECT_PATH_NOT_FOUND:
		return windowsNativeErrorNotFound
	case windows.STATUS_OBJECT_NAME_COLLISION, windows.STATUS_OBJECT_NAME_EXISTS:
		return windowsNativeErrorCollision
	case windows.STATUS_SHARING_VIOLATION:
		return windowsNativeErrorSharing
	case windows.STATUS_INVALID_INFO_CLASS, windows.STATUS_NOT_IMPLEMENTED, windows.STATUS_NOT_SUPPORTED,
		windows.STATUS_EAS_NOT_SUPPORTED:
		return windowsNativeErrorUnsupported
	default:
		return classifyWindowsNativeError(status.Errno())
	}
}

func windowsNativeErrorClassOf(err error) windowsNativeErrorClass {
	var native *windowsNativeError
	if errors.As(err, &native) {
		return native.class
	}
	return classifyWindowsNativeError(err)
}

func windowsNativeUnsupported(phase windowsNativePhase, detail string, cause error) error {
	if detail == "" {
		detail = "Windows storage capability is unavailable"
	}
	unsupportedCause := fmt.Errorf("%w: %s", errWindowsNativeUnsupported, detail)
	if cause != nil {
		unsupportedCause = errors.Join(unsupportedCause, cause)
	}
	return normalizeWindowsNativeError(phase, unsupportedCause, false)
}
