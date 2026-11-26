package record

import (
	"crypto/sha256"
	"fmt"
)

const (
	FileName               = "journal.json"
	Mode                   = 0o600
	MaximumBytes     int64 = 64 << 20
	MaximumJSONDepth       = 64
	OperationApply         = "apply"

	// Journal versions bind the canonical state encoding used by authority hashes.
	OrdinaryVersion = 15
	native16Version = 16
	NativeVersion   = native17Version
	// Earlier readers cannot execute the absent-root probe when undoing an
	// ordinary projection that deleted its physical document.
	ordinaryRootRestoreVersion = 18
	nativeRootRestoreVersion   = 19
	ordinaryInstalledVersion   = 20
	nativeInstalledVersion     = 21
	nativeWholeModeVersion     = 22
)

func SupportsOrdinaryVersion(version int) bool {
	return version == OrdinaryVersion || version == ordinaryRootRestoreVersion || version == ordinaryInstalledVersion
}

func SupportsNativeVersion(version int) bool {
	return version == native16Version || boundNativeHostVersion(version)
}

func boundNativeHostVersion(version int) bool {
	return version == native17Version || version == nativeRootRestoreVersion || version == nativeInstalledVersion || version == nativeWholeModeVersion
}

// Fingerprint identifies exact serialized bytes, not validation or host authority.
func Fingerprint(content []byte) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256(content))
}
