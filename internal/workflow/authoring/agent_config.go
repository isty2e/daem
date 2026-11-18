package authoring

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/agentconfig"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

type AddAgentConfigRequest struct {
	ID             string
	SourceArg      string
	RepositoryPath string
	Ref            string
	VersionID      string
	Region         string
	Target         target.Target
	Scope          target.Scope
	ConfigPath     string
	Ownership      string
	Syntax         string
}

type RemoveAgentConfigRequest struct {
	ID     string
	Target target.Target
	Scope  target.Scope
}

func AddAgentConfig(ctx context.Context, options ExecutionOptions, request AddAgentConfigRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return buildAddAgentConfigChange(document, request)
	})
}

func RemoveAgentConfig(ctx context.Context, options ExecutionOptions, request RemoveAgentConfigRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return buildRemoveAgentConfigChange(document, request)
	})
}

func buildAddAgentConfigChange(document ManifestDocument, request AddAgentConfigRequest) (Change, error) {
	environment, err := declarationmanifest.Decode(document.Content)
	if err != nil {
		return Change{}, fmt.Errorf("invalid manifest: %w", err)
	}
	provenance, err := agentConfigAuthoringSource(request, document.Root)
	if err != nil {
		return Change{}, err
	}
	requested, err := agentconfig.New(agentconfig.Spec{
		Name: request.ID, Source: provenance, Target: request.Target, Scope: request.Scope,
		Path: request.ConfigPath, Ownership: agentconfig.Ownership(request.Ownership), Syntax: agentconfig.Syntax(request.Syntax),
	})
	if err != nil {
		return Change{}, err
	}

	change := Change{ManifestPath: document.Path, Original: document.Content, ResourceID: requested.ID().Name()}
	before := environment.AgentConfigs()
	if index := agentConfigIndex(before, requested.ID()); index >= 0 {
		if !sameAgentConfigDeclaration(document.Root, before[index], requested) {
			return Change{}, fmt.Errorf("agent configuration %q already exists with different settings or source", requested.ID().Name())
		}
		change.Content = document.Content
		change.ChangeKind = "unchanged"
		return change, nil
	}

	change.Content, change.ManifestBlock, err = declarationcodec.AppendAgentConfigDeclaration(document.Content, requested)
	if err != nil {
		return Change{}, err
	}
	result, err := declarationmanifest.Decode(change.Content)
	if err != nil {
		return Change{}, fmt.Errorf("resulting manifest is invalid: %w", err)
	}
	after := result.AgentConfigs()
	if len(after) != len(before)+1 || !slices.Equal(before, after[:len(before)]) ||
		!sameAgentConfigDeclaration(document.Root, after[len(before)], requested) {
		return Change{}, fmt.Errorf("manifest edit did not add only the requested agent configuration")
	}
	change.ChangeKind = "append agent-config resource"
	return change, nil
}

func buildRemoveAgentConfigChange(document ManifestDocument, request RemoveAgentConfigRequest) (Change, error) {
	environment, err := declarationmanifest.Decode(document.Content)
	if err != nil {
		return Change{}, fmt.Errorf("invalid manifest: %w", err)
	}
	id, err := entity.New(entity.KindAgentConfig, request.ID)
	if err != nil {
		return Change{}, err
	}
	configs := environment.AgentConfigs()
	index := agentConfigIndex(configs, id)
	if index < 0 || !matchesAgentConfigSelection(configs[index], request) {
		available := make([]ResourceSelection, 0, len(configs))
		for _, config := range configs {
			available = append(available, ResourceSelection{
				Name: config.ID().Name(), Scope: string(config.Scope()), Targets: []string{string(config.Target())},
			})
		}
		return Change{}, missingResourceSelection(entity.KindAgentConfig, request.ID, available)
	}
	changeKind := "remove selected-keys agent-config declaration"
	if configs[index].Ownership() == agentconfig.OwnershipDocument {
		changeKind = "remove whole-file agent-config declaration"
	}

	content, err := declarationcodec.RemoveAgentConfigDeclaration(document.Content, id.Name())
	if err != nil {
		return Change{}, err
	}
	result, err := declarationmanifest.Decode(content)
	if err != nil {
		return Change{}, fmt.Errorf("resulting manifest is invalid: %w", err)
	}
	if !slices.Equal(slices.Delete(configs, index, index+1), result.AgentConfigs()) {
		return Change{}, fmt.Errorf("manifest edit did not remove only agent configuration %q", id.Name())
	}
	return Change{
		ManifestPath: document.Path, Original: document.Content, Content: content,
		ResourceID: id.Name(), ChangeKind: changeKind,
	}, nil
}

func agentConfigIndex(configs []agentconfig.Config, id entity.ID) int {
	return slices.IndexFunc(configs, func(config agentconfig.Config) bool { return config.ID() == id })
}

func matchesAgentConfigSelection(config agentconfig.Config, request RemoveAgentConfigRequest) bool {
	return (request.Target == "" || request.Target == config.Target()) &&
		(request.Scope == "" || request.Scope == config.Scope())
}

func sameAgentConfigDeclaration(root string, left, right agentconfig.Config) bool {
	if left.ID() != right.ID() || left.Target() != right.Target() || left.Scope() != right.Scope() ||
		left.Path() != right.Path() || left.Ownership() != right.Ownership() || left.Syntax() != right.Syntax() {
		return false
	}
	if local, ok := left.Source().Local(); ok {
		other, ok := right.Source().Local()
		return ok && localAgentConfigFile(root, local) == localAgentConfigFile(root, other)
	}
	leftID, leftErr := source.SourceIDFor(left.Source())
	rightID, rightErr := source.SourceIDFor(right.Source())
	return leftErr == nil && rightErr == nil && leftID == rightID
}

func localAgentConfigFile(root string, local source.LocalSource) string {
	if local.IsAbsolute() {
		return filepath.Clean(local.Path())
	}
	return filepath.Join(root, local.Path())
}

func agentConfigAuthoringSource(request AddAgentConfigRequest, root string) (source.Source, error) {
	operand := strings.TrimSpace(request.SourceArg)
	gitOptions := request.RepositoryPath != "" || request.Ref != ""
	s3Options := request.VersionID != "" || request.Region != ""
	if strings.HasPrefix(operand, "s3://") {
		if gitOptions {
			return source.Source{}, fmt.Errorf("S3 sources do not accept --path or --ref")
		}
		return source.NewS3Source(operand, request.VersionID, request.Region, source.S3ObjectFormatFile)
	}
	if s3Options {
		return source.Source{}, fmt.Errorf("--version-id and --region require an S3 source")
	}
	if gitOptions || explicitAgentConfigGitLocator(operand) {
		git, err := gitAuthoringSource(operand, request.RepositoryPath, request.Ref)
		if err != nil {
			return source.Source{}, err
		}
		selected, _ := git.Git()
		if selected.RepositoryPath().String() == "." {
			return source.Source{}, fmt.Errorf("Git sources for agent configuration require a file path: use --path or owner/repo/path shorthand")
		}
		return git, nil
	}
	if operand == "" {
		return source.Source{}, fmt.Errorf("agent configuration source is required")
	}
	path, err := authoringLocalFilePath(operand, root, request.Scope)
	if err != nil {
		return source.Source{}, err
	}
	return source.NewLocalSource(path, source.LocalSourceModeVendor)
}

func explicitAgentConfigGitLocator(operand string) bool {
	if filepath.IsAbs(operand) || strings.HasPrefix(operand, "./") || strings.HasPrefix(operand, "../") {
		return false
	}
	if strings.Contains(operand, "://") {
		return true
	}
	locator, err := source.ParseGitLocator(operand)
	return err == nil && !locator.IsNativeLocal()
}
