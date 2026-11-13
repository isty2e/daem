package authoring

import (
	"errors"
	"fmt"
	"strings"

	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/target"
)

var (
	ErrHostConfigManagementNotFound  = errors.New("host configuration management not found")
	ErrHostConfigManagementAmbiguous = errors.New("host configuration management is ambiguous")
)

type UnmanageHostConfigRequest struct {
	ManifestPath string
	ID           string
	Target       target.Target
	Scope        target.Scope
	Mode         UnmanageMode
}

func (request UnmanageHostConfigRequest) validate() error {
	if _, err := entity.New(entity.KindHostConfig, request.ID); err != nil {
		return err
	}
	if strings.IndexFunc(request.ID, func(r rune) bool { return r < ' ' || r == 127 }) >= 0 {
		return fmt.Errorf("host configuration id contains control characters")
	}
	if request.Target != "" {
		if _, err := target.ParseTarget(string(request.Target)); err != nil {
			return err
		}
	}
	if request.Scope != "" {
		if _, err := target.ParseScope(string(request.Scope)); err != nil {
			return err
		}
	}
	if request.Mode != UnmanageModeWrite && request.Mode != UnmanageModeDryRun {
		return fmt.Errorf("unmanage mode %q is invalid", request.Mode)
	}
	return nil
}

type UnmanageHostConfigResult struct {
	ManifestPath     string
	LockfilePath     string
	StatefilePath    string
	RegistryPath     string
	ResourceID       string
	Target           target.Target
	Scope            target.Scope
	Original         []byte
	Content          []byte
	ManifestStatus   UnmanageManifestStatus
	LockfileStatus   LockfileStatus
	ManagementStatus UnmanageManagementStatus
	StatefileStatus  UnmanageStateStatus
	RegistryStatus   UnmanageStateStatus
	Mode             UnmanageMode
}
