package authoring

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	declarationmanifest "github.com/isty2e/daem/internal/declaration/manifest"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

type AddHostConfigRequest struct {
	ID         string
	SourceArg  string
	SourcePath string
	Ref        string
	VersionID  string
	Region     string
	Target     target.Target
	Scope      target.Scope
	HostPath   string
	Ownership  string
	Syntax     string
}

type RemoveHostConfigRequest struct {
	ID     string
	Target target.Target
	Scope  target.Scope
}

func AddHostConfig(ctx context.Context, options ExecutionOptions, request AddHostConfigRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return buildAddHostConfigChange(document, request)
	})
}

func RemoveHostConfig(ctx context.Context, options ExecutionOptions, request RemoveHostConfigRequest) (OperationResult, error) {
	return executeAuthoringOperation(ctx, options, func(document ManifestDocument) (Change, error) {
		return buildRemoveHostConfigChange(document, request)
	})
}

func buildAddHostConfigChange(document ManifestDocument, request AddHostConfigRequest) (Change, error) {
	environment, err := declarationmanifest.Decode(document.Content)
	if err != nil {
		return Change{}, fmt.Errorf("invalid manifest: %w", err)
	}
	provenance, err := hostConfigAuthoringSource(request, document.Root)
	if err != nil {
		return Change{}, err
	}
	requested, err := hostconfig.New(hostconfig.Spec{
		Name: request.ID, Source: provenance, Target: request.Target, Scope: request.Scope,
		Path: request.HostPath, Ownership: hostconfig.Ownership(request.Ownership), Syntax: hostconfig.Syntax(request.Syntax),
	})
	if err != nil {
		return Change{}, err
	}

	change := Change{ManifestPath: document.Path, Original: document.Content, ResourceID: requested.ID().Name()}
	configs := environment.HostConfigs()
	if index := hostConfigIndex(configs, requested.ID()); index >= 0 {
		if !sameHostConfigDeclaration(document.Root, configs[index], requested) {
			return Change{}, fmt.Errorf("host configuration %q already exists with different settings or source", requested.ID().Name())
		}
		change.Content = document.Content
		change.ChangeKind = "unchanged"
		return change, nil
	}

	change.Content, change.ManifestBlock, err = declarationcodec.AppendHostConfigDeclaration(document.Content, requested)
	if err != nil {
		return Change{}, err
	}
	result, err := declarationmanifest.Decode(change.Content)
	if err != nil {
		return Change{}, fmt.Errorf("resulting manifest is invalid: %w", err)
	}
	configs = result.HostConfigs()
	index := hostConfigIndex(configs, requested.ID())
	if index < 0 || !sameHostConfigDeclaration(document.Root, configs[index], requested) {
		return Change{}, fmt.Errorf("manifest edit did not add the requested host configuration")
	}
	change.ChangeKind = "append host-config resource"
	return change, nil
}

func buildRemoveHostConfigChange(document ManifestDocument, request RemoveHostConfigRequest) (Change, error) {
	environment, err := declarationmanifest.Decode(document.Content)
	if err != nil {
		return Change{}, fmt.Errorf("invalid manifest: %w", err)
	}
	id, err := entity.New(entity.KindHostConfig, request.ID)
	if err != nil {
		return Change{}, err
	}
	configs := environment.HostConfigs()
	index := hostConfigIndex(configs, id)
	if index < 0 || !matchesHostConfigSelection(configs[index], request) {
		available := make([]ResourceSelection, 0, len(configs))
		for _, config := range configs {
			available = append(available, ResourceSelection{
				Name: config.ID().Name(), Scope: string(config.Scope()), Targets: []string{string(config.Target())},
			})
		}
		return Change{}, missingResourceSelection(entity.KindHostConfig, request.ID, available)
	}

	content, err := declarationcodec.RemoveHostConfigDeclaration(document.Content, id.Name())
	if err != nil {
		return Change{}, err
	}
	result, err := declarationmanifest.Decode(content)
	if err != nil {
		return Change{}, fmt.Errorf("resulting manifest is invalid: %w", err)
	}
	if hostConfigIndex(result.HostConfigs(), id) >= 0 {
		return Change{}, fmt.Errorf("host configuration %q remains after the manifest edit", id.Name())
	}
	return Change{
		ManifestPath: document.Path, Original: document.Content, Content: content,
		ResourceID: id.Name(), ChangeKind: "remove host-config resource",
	}, nil
}

func hostConfigIndex(configs []hostconfig.Config, id entity.ID) int {
	return slices.IndexFunc(configs, func(config hostconfig.Config) bool { return config.ID() == id })
}

func matchesHostConfigSelection(config hostconfig.Config, request RemoveHostConfigRequest) bool {
	return (request.Target == "" || request.Target == config.Target()) &&
		(request.Scope == "" || request.Scope == config.Scope())
}

func sameHostConfigDeclaration(root string, left, right hostconfig.Config) bool {
	if left.ID() != right.ID() || left.Target() != right.Target() || left.Scope() != right.Scope() ||
		left.Path() != right.Path() || left.Ownership() != right.Ownership() || left.Syntax() != right.Syntax() {
		return false
	}
	if local, ok := left.Source().Local(); ok {
		other, ok := right.Source().Local()
		return ok && localHostConfigFile(root, local) == localHostConfigFile(root, other)
	}
	leftID, leftErr := source.SourceIDFor(left.Source())
	rightID, rightErr := source.SourceIDFor(right.Source())
	return leftErr == nil && rightErr == nil && leftID == rightID
}

func localHostConfigFile(root string, local source.LocalSource) string {
	if local.IsAbsolute() {
		return filepath.Clean(local.Path())
	}
	return filepath.Join(root, local.Path())
}

func hostConfigAuthoringSource(request AddHostConfigRequest, root string) (source.Source, error) {
	operand := strings.TrimSpace(request.SourceArg)
	gitOptions := request.SourcePath != "" || request.Ref != ""
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
	if gitOptions || explicitHostConfigGitLocator(operand) {
		git, err := gitAuthoringSource(operand, request.SourcePath, request.Ref)
		if err != nil {
			return source.Source{}, err
		}
		selected, _ := git.Git()
		if selected.RepositoryPath().String() == "." {
			return source.Source{}, fmt.Errorf("Git host configuration sources require a file path: use --path or owner/repo/path shorthand")
		}
		return git, nil
	}
	if operand == "" {
		return source.Source{}, fmt.Errorf("host configuration source is required")
	}
	path, err := authoringLocalFilePath(operand, root, request.Scope)
	if err != nil {
		return source.Source{}, err
	}
	return source.NewLocalSource(path, source.LocalSourceModeVendor)
}

func explicitHostConfigGitLocator(operand string) bool {
	if filepath.IsAbs(operand) || strings.HasPrefix(operand, "./") || strings.HasPrefix(operand, "../") {
		return false
	}
	if strings.Contains(operand, "://") {
		return true
	}
	locator, err := source.ParseGitLocator(operand)
	return err == nil && !locator.IsNativeLocal()
}
