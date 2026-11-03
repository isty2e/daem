package adopt

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

type HostConfigInitialCommandInput struct {
	ManifestPath, Name, Target, Scope, HostPath string
	Ownership, Syntax, SourceDir                string
	Keys                                        []string
	Merge                                       bool
}

type HostConfigUpdateCommandInput struct {
	ManifestPath, Name, SourcePath string
	Keys                           []string
}

type HostConfigCaptureCommand struct {
	capture  *HostConfigCapturePlan
	recovery *HostConfigCaptureRecovery
}

func (command HostConfigCaptureCommand) Capture() *HostConfigCapturePlan { return command.capture }
func (command HostConfigCaptureCommand) Recovery() *HostConfigCaptureRecovery {
	return command.recovery
}

type captureCommandInputError struct{ cause error }

func (err captureCommandInputError) Error() string { return err.cause.Error() }
func (err captureCommandInputError) Unwrap() error { return err.cause }
func IsHostConfigCaptureInputError(err error) bool {
	var input captureCommandInputError
	return errors.As(err, &input)
}

func PlanHostConfigInitialCapture(ctx context.Context, request HostConfigInitialCommandInput) (HostConfigCaptureCommand, error) {
	input, err := normalizeInitialCapture(request)
	if err != nil {
		return HostConfigCaptureCommand{}, captureCommandInputError{err}
	}
	path, err := localCaptureSourcePath(input.Paths.ManifestRoot, input.Config.Source())
	if err != nil {
		return HostConfigCaptureCommand{}, err
	}
	recovery, err := InspectHostConfigCaptureRecovery(ctx, HostConfigCaptureRecoveryInput{Paths: input.Paths, Name: request.Name, SourcePath: path})
	if err != nil {
		return HostConfigCaptureCommand{}, err
	}
	if recovery != nil {
		return HostConfigCaptureCommand{recovery: recovery}, nil
	}
	plan, err := BuildHostConfigInitialPlan(ctx, input)
	return HostConfigCaptureCommand{capture: plan}, err
}

func PlanHostConfigUpdateCapture(ctx context.Context, request HostConfigUpdateCommandInput) (HostConfigCaptureCommand, error) {
	keys, err := captureCommandKeys(request.Keys)
	if err != nil {
		return HostConfigCaptureCommand{}, captureCommandInputError{err}
	}
	paths, err := daempaths.Resolve(request.ManifestPath)
	if err != nil {
		return HostConfigCaptureCommand{}, captureCommandInputError{err}
	}
	recovery, err := InspectHostConfigCaptureRecovery(ctx, HostConfigCaptureRecoveryInput{Paths: paths, Name: request.Name, SourcePath: request.SourcePath})
	if err != nil {
		return HostConfigCaptureCommand{}, err
	}
	if recovery != nil {
		return HostConfigCaptureCommand{recovery: recovery}, nil
	}
	plan, err := BuildHostConfigUpdatePlan(ctx, HostConfigUpdateInput{Paths: paths, Name: request.Name, SourcePath: request.SourcePath, Keys: keys})
	return HostConfigCaptureCommand{capture: plan}, err
}

func normalizeInitialCapture(request HostConfigInitialCommandInput) (HostConfigInitialInput, error) {
	keys, err := captureCommandKeys(request.Keys)
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	var paths daempaths.Paths
	if request.Merge {
		paths, err = daempaths.Resolve(request.ManifestPath)
	} else {
		paths, err = daempaths.ResolveCreation(request.ManifestPath)
	}
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	selectedTarget, err := target.ParseTarget(request.Target)
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	selectedScope, err := target.ParseScope(request.Scope)
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	mode, err := hostconfig.ParseOwnership(request.Ownership)
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	grammar, err := hostconfig.ParseSyntax(request.Syntax)
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	if mode == hostconfig.OwnershipKeys && len(keys) == 0 || mode == hostconfig.OwnershipDocument && len(keys) != 0 {
		return HostConfigInitialInput{}, fmt.Errorf("initial capture requires keys or explicit --ownership document, not both")
	}
	directory := request.SourceDir
	if directory == "" {
		directory = DefaultSourceDir(filepath.Base(paths.ManifestPath))
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(paths.ManifestRoot, directory)
	}
	extension := filepath.Ext(request.HostPath)
	if extension == "" {
		extension = ".conf"
	}
	path := filepath.Join(directory, "host-config", url.PathEscape(request.Name)+extension)
	local, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	config, err := hostconfig.New(hostconfig.Spec{Name: request.Name, Source: local, Target: selectedTarget, Scope: selectedScope, Path: request.HostPath, Ownership: mode, Syntax: grammar})
	if err != nil {
		return HostConfigInitialInput{}, err
	}
	return HostConfigInitialInput{Paths: paths, Config: config, Keys: keys, Merge: request.Merge}, nil
}

func captureCommandKeys(pointers []string) ([]output.KeyPath, error) {
	keys := make([]output.KeyPath, 0, len(pointers))
	for _, pointer := range pointers {
		key, err := output.ParseKeyPath(pointer)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}
