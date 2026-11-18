package adopt

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/output"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

type AgentConfigInitialCommandInput struct {
	ManifestPath, Name, Target, Scope, ConfigPath string
	Ownership, Syntax, SourceDir                  string
	Keys                                          []string
	Merge                                         bool
}

type AgentConfigUpdateCommandInput struct {
	ManifestPath, Name, SourcePath string
	Keys                           []string
}

type AgentConfigCaptureCommand struct {
	capture  *AgentConfigCapturePlan
	recovery *AgentConfigCaptureRecovery
}

func (command AgentConfigCaptureCommand) Capture() *AgentConfigCapturePlan { return command.capture }

func (command AgentConfigCaptureCommand) Recovery() *AgentConfigCaptureRecovery {
	return command.recovery
}

type captureCommandInputError struct{ cause error }

func (err captureCommandInputError) Error() string { return err.cause.Error() }
func (err captureCommandInputError) Unwrap() error { return err.cause }
func IsAgentConfigCaptureInputError(err error) bool {
	var input captureCommandInputError
	return errors.As(err, &input)
}

func PlanAgentConfigInitialCapture(ctx context.Context, request AgentConfigInitialCommandInput) (AgentConfigCaptureCommand, error) {
	input, err := normalizeInitialCapture(request)
	if err != nil {
		return AgentConfigCaptureCommand{}, captureCommandInputError{err}
	}
	path, err := localCaptureSourcePath(input.Paths.ManifestRoot, input.Config.Source())
	if err != nil {
		return AgentConfigCaptureCommand{}, err
	}
	recovery, err := InspectAgentConfigCaptureRecovery(ctx, AgentConfigCaptureRecoveryInput{Paths: input.Paths, Name: request.Name, SourcePath: path})
	if err != nil {
		return AgentConfigCaptureCommand{}, err
	}
	if recovery != nil {
		return AgentConfigCaptureCommand{recovery: recovery}, nil
	}
	plan, err := BuildAgentConfigInitialPlan(ctx, input)
	return AgentConfigCaptureCommand{capture: plan}, err
}

func PlanAgentConfigUpdateCapture(ctx context.Context, request AgentConfigUpdateCommandInput) (AgentConfigCaptureCommand, error) {
	keys, err := captureCommandKeys(request.Keys)
	if err != nil {
		return AgentConfigCaptureCommand{}, captureCommandInputError{err}
	}
	paths, err := daempaths.Resolve(request.ManifestPath)
	if err != nil {
		return AgentConfigCaptureCommand{}, captureCommandInputError{err}
	}
	recovery, err := InspectAgentConfigCaptureRecovery(ctx, AgentConfigCaptureRecoveryInput{Paths: paths, Name: request.Name, SourcePath: request.SourcePath})
	if err != nil {
		return AgentConfigCaptureCommand{}, err
	}
	if recovery != nil {
		return AgentConfigCaptureCommand{recovery: recovery}, nil
	}
	plan, err := BuildAgentConfigUpdatePlan(ctx, AgentConfigUpdateInput{Paths: paths, Name: request.Name, SourcePath: request.SourcePath, Keys: keys})
	return AgentConfigCaptureCommand{capture: plan}, err
}

func normalizeInitialCapture(request AgentConfigInitialCommandInput) (AgentConfigInitialInput, error) {
	keys, err := captureCommandKeys(request.Keys)
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	var paths daempaths.Paths
	if request.Merge {
		paths, err = daempaths.Resolve(request.ManifestPath)
	} else {
		paths, err = daempaths.ResolveCreation(request.ManifestPath)
	}
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	selectedTarget, err := target.ParseTarget(request.Target)
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	selectedScope, err := target.ParseScope(request.Scope)
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	mode, err := agentconfig.ParseOwnership(request.Ownership)
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	grammar, err := agentconfig.ParseSyntax(request.Syntax)
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	if mode == agentconfig.OwnershipKeys && len(keys) == 0 || mode == agentconfig.OwnershipDocument && len(keys) != 0 {
		return AgentConfigInitialInput{}, fmt.Errorf("initial capture requires keys or explicit --ownership document, not both")
	}
	directory := request.SourceDir
	if directory == "" {
		directory = DefaultSourceDir(filepath.Base(paths.ManifestPath))
	}
	if !filepath.IsAbs(directory) {
		directory = filepath.Join(paths.ManifestRoot, directory)
	}
	extension := filepath.Ext(request.ConfigPath)
	if extension == "" {
		extension = ".conf"
	}
	path := filepath.Join(directory, "agent-config", url.PathEscape(request.Name)+extension)
	local, err := source.NewLocalSource(path, source.LocalSourceModeVendor)
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	config, err := agentconfig.New(agentconfig.Spec{Name: request.Name, Source: local, Target: selectedTarget, Scope: selectedScope, Path: request.ConfigPath, Ownership: mode, Syntax: grammar})
	if err != nil {
		return AgentConfigInitialInput{}, err
	}
	return AgentConfigInitialInput{Paths: paths, Config: config, Keys: keys, Merge: request.Merge}, nil
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
