package authoring

import (
	"errors"
	"fmt"

	"github.com/isty2e/daem/internal/target"
)

var (
	// ErrNotFound means neither a selected declaration nor selected daem
	// management facts exist.
	ErrUnmanageExtensionNotFound = errors.New("extension management not found")
	// ErrAmbiguous means the supplied safety filters do not select one relation.
	ErrUnmanageExtensionAmbiguous = errors.New("extension management is ambiguous")
)

// Mode selects preview or durable metadata mutation.
type UnmanageMode string

const (
	UnmanageModeDryRun UnmanageMode = "dry-run"
	UnmanageModeWrite  UnmanageMode = "write"
)

// UnmanageExtensionRequest identifies one extension relation whose host state
// must be retained.
type UnmanageExtensionRequest struct {
	ManifestPath string
	LockfilePath string
	ID           string
	Target       target.Target
	Scope        target.Scope
	Mode         UnmanageMode
}

func (request UnmanageExtensionRequest) validate() (UnmanageExtensionRequest, error) {
	id, err := CleanExtensionID(request.ID)
	if err != nil {
		return UnmanageExtensionRequest{}, err
	}
	request.ID = id
	if request.Target != "" {
		if _, err := target.ParseTarget(string(request.Target)); err != nil {
			return UnmanageExtensionRequest{}, err
		}
	}
	if request.Scope != "" {
		if _, err := target.ParseScope(string(request.Scope)); err != nil {
			return UnmanageExtensionRequest{}, err
		}
	}
	switch request.Mode {
	case UnmanageModeDryRun, UnmanageModeWrite:
		return request, nil
	default:
		return UnmanageExtensionRequest{}, fmt.Errorf("unmanage mode %q is invalid", request.Mode)
	}
}

// UnmanageManifestStatus describes the declaration transition.
type UnmanageManifestStatus string

const (
	UnmanageManifestStatusWouldRemove UnmanageManifestStatus = "would_remove"
	UnmanageManifestStatusRemoved     UnmanageManifestStatus = "removed"
	UnmanageManifestStatusUnchanged   UnmanageManifestStatus = "unchanged"
)

// UnmanageManagementStatus describes the exact daem claim/pending-fact transition.
type UnmanageManagementStatus string

const (
	UnmanageManagementStatusWouldRelease UnmanageManagementStatus = "would_release"
	UnmanageManagementStatusReleased     UnmanageManagementStatus = "released"
	UnmanageManagementStatusNotPresent   UnmanageManagementStatus = "not_present"
)

// UnmanageStateStatus describes a durable state or registry transition.
type UnmanageStateStatus string

const (
	UnmanageStateStatusWouldWrite UnmanageStateStatus = "would_write"
	UnmanageStateStatusWritten    UnmanageStateStatus = "written"
	UnmanageStateStatusUnchanged  UnmanageStateStatus = "unchanged"
)

// UnmanageExtensionResult contains public facts for one host-preserving operation.
type UnmanageExtensionResult struct {
	ManifestPath                 string
	LockfilePath                 string
	StatefilePath                string
	RegistryPath                 string
	Original                     []byte
	Content                      []byte
	ResourceID                   string
	Target                       target.Target
	Scope                        target.Scope
	ManifestStatus               UnmanageManifestStatus
	LockfileStatus               LockfileStatus
	ManagementStatus             UnmanageManagementStatus
	StatefileStatus              UnmanageStateStatus
	RegistryStatus               UnmanageStateStatus
	HostStateRetained            bool
	AmbientConsumersUnobservable bool
	DeclarationPresent           bool
	Mode                         UnmanageMode
}
