package probe

import (
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/subprocess"
)

func projectWorkingDirectoryBinder(selectedRoot string) subprocess.WorkingDirectoryBinder {
	return func() (subprocess.WorkingDirectoryBinding, error) {
		root, err := rootedpath.CaptureRoot(selectedRoot)
		if err != nil {
			return nil, fmt.Errorf("capture selected project root: %w", err)
		}
		if err := root.ValidateSelection(selectedRoot); err != nil {
			return nil, errors.Join(err, root.Close())
		}
		capability, err := root.AcquireWorkingDirectory()
		if err != nil {
			return nil, errors.Join(err, root.Close())
		}
		return &projectWorkingDirectoryBinding{
			selectedRoot: selectedRoot,
			root:         root,
			capability:   capability,
		}, nil
	}
}

type projectWorkingDirectoryBinding struct {
	selectedRoot string
	root         *rootedpath.CapturedRoot
	capability   rootedpath.WorkingDirectoryCapability
}

func (binding *projectWorkingDirectoryBinding) Validate() error {
	if binding == nil || binding.root == nil || binding.capability == nil {
		return fmt.Errorf("project working-directory binding is incomplete")
	}
	if err := binding.root.ValidateSelection(binding.selectedRoot); err != nil {
		return err
	}
	return binding.capability.Validate()
}

func (binding *projectWorkingDirectoryBinding) OpenDirectory() (*os.File, error) {
	if binding == nil || binding.capability == nil {
		return nil, fmt.Errorf("project working-directory binding is incomplete")
	}
	return binding.capability.OpenDirectory()
}

func (binding *projectWorkingDirectoryBinding) Close() error {
	if binding == nil {
		return nil
	}
	var capabilityErr error
	if binding.capability != nil {
		capabilityErr = binding.capability.Close()
		binding.capability = nil
	}
	var rootErr error
	if binding.root != nil {
		rootErr = binding.root.Close()
		binding.root = nil
	}
	return errors.Join(capabilityErr, rootErr)
}
