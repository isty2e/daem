// Package buildidentity owns the immutable provenance facts embedded in one
// daem executable and the intrinsic facts required of a release build.
package buildidentity

import (
	"runtime"
	"runtime/debug"
	"time"

	"github.com/isty2e/daem/internal/platformsupport"
)

const (
	// MainPackagePath is the only executable package admitted for daem releases.
	MainPackagePath = "github.com/isty2e/daem/cmd/daem"
	// MainModulePath is the canonical module that owns the daem executable.
	MainModulePath = "github.com/isty2e/daem"
)

// SourceState records whether the source tree was modified when the executable
// was built. Unknown never implies clean.
type SourceState uint8

const (
	SourceUnknown SourceState = iota
	SourceClean
	SourceModified
)

// String returns the stable source-state label.
func (state SourceState) String() string {
	switch state {
	case SourceClean:
		return "clean"
	case SourceModified:
		return "modified"
	default:
		return "unknown"
	}
}

// CGOState records the embedded CGO_ENABLED build setting.
type CGOState uint8

const (
	CGOUnknown CGOState = iota
	CGODisabled
	CGOEnabled
)

// String returns the stable CGO state label.
func (state CGOState) String() string {
	switch state {
	case CGODisabled:
		return "disabled"
	case CGOEnabled:
		return "enabled"
	default:
		return "unknown"
	}
}

// Identity is the canonical embedded identity of one executable. Its zero
// value is a truthful identity with every fact unknown.
type Identity struct {
	mainPackage   string
	mainModule    string
	version       string
	vcs           string
	revision      string
	revisionAt    time.Time
	hasRevisionAt bool
	sourceState   SourceState
	goVersion     string
	target        platformsupport.Target
	cgo           CGOState
}

// ReleaseRequirement is the validated external identity a release lane must
// match. It cannot repair or override facts embedded in the executable.
type ReleaseRequirement struct {
	version   string
	revision  string
	goVersion string
}

// Version returns the required canonical release version.
func (requirement ReleaseRequirement) Version() string { return requirement.version }

// Current returns the running executable's identity. Missing or malformed
// embedded metadata degrades to explicit unknown provenance while preserving
// runtime-owned toolchain and target facts.
func Current() Identity {
	info, ok := debug.ReadBuildInfo()
	if ok {
		identity, err := FromBuildInfo(*info)
		if err == nil {
			return identity
		}
	}

	target, err := platformsupport.ParseTarget(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Identity{goVersion: runtime.Version()}
	}
	return Identity{goVersion: runtime.Version(), target: target}
}

// Version returns the embedded module version, or an empty string when unknown.
func (identity Identity) Version() string { return identity.version }

// VCS returns the embedded version-control kind, or an empty string when unknown.
func (identity Identity) VCS() string { return identity.vcs }

// Revision returns the embedded source revision, or an empty string when unknown.
func (identity Identity) Revision() string { return identity.revision }

// RevisionTime returns the embedded source revision time and whether it is known.
func (identity Identity) RevisionTime() (time.Time, bool) {
	return identity.revisionAt, identity.hasRevisionAt
}

// SourceState returns the embedded clean, modified, or unknown source state.
func (identity Identity) SourceState() SourceState { return identity.sourceState }

// GoVersion returns the embedded Go toolchain version, or an empty string when unknown.
func (identity Identity) GoVersion() string { return identity.goVersion }

// Target returns the embedded canonical build target, or the zero target when unknown.
func (identity Identity) Target() platformsupport.Target { return identity.target }
