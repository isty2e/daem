package authoring

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/declaration"
	declarationcodec "github.com/isty2e/daem/internal/declaration/codec"
	sourcepkg "github.com/isty2e/daem/internal/supply/source"
	"github.com/isty2e/daem/internal/target"
)

// ErrMissingGitRef identifies Git authoring input without an explicit ref.
var ErrMissingGitRef = errors.New("--ref is required for git sources")

func DefaultLocalSourceMode() string {
	return string(sourcepkg.LocalSourceModeVendor)
}

func skillSource(request AddSkillRequest, manifestRoot string) (declarationcodec.SkillSource, string, error) {
	if strings.TrimSpace(request.SourceArg) == "" {
		return declarationcodec.SkillSource{}, "", fmt.Errorf("missing skill source")
	}
	if skillSourceLooksGit(request.SourceArg, request.SourcePath, request.Ref) {
		return gitSkillSource(request)
	}
	return localSkillSource(request, manifestRoot)
}

func gitSkillSource(request AddSkillRequest) (declarationcodec.SkillSource, string, error) {
	if request.Mode != "" && request.Mode != string(sourcepkg.LocalSourceModeVendor) {
		return declarationcodec.SkillSource{}, "", fmt.Errorf("--mode is only valid for local sources")
	}
	source, err := gitSourceDeclaration(request.SourceArg, request.SourcePath, request.Ref)
	if err != nil {
		return declarationcodec.SkillSource{}, "", err
	}
	inferredName := path.Base(source.Path)
	if source.Path == "." {
		inferredName = gitRootSkillName(source.Git)
	}

	return declarationcodec.SkillSource{Git: source.Git, Path: source.Path, Ref: source.Ref}, inferredName, nil
}

func localSkillSource(request AddSkillRequest, manifestRoot string) (declarationcodec.SkillSource, string, error) {
	if request.Ref != "" {
		return declarationcodec.SkillSource{}, "", fmt.Errorf("--ref is only valid for git sources")
	}
	if request.SourcePath != "" {
		return declarationcodec.SkillSource{}, "", fmt.Errorf("--path is only valid for git sources")
	}
	mode := request.Mode
	if mode == "" {
		mode = string(sourcepkg.LocalSourceModeVendor)
	}
	if _, err := sourcepkg.ParseLocalSourceMode(mode); err != nil {
		return declarationcodec.SkillSource{}, "", err
	}

	absoluteSource, err := filepath.Abs(request.SourceArg)
	if err != nil {
		return declarationcodec.SkillSource{}, "", fmt.Errorf("resolve local source: %w", err)
	}
	manifestPath := absoluteSource
	if request.Scope != string(target.ScopeGlobal) {
		if relative, ok := relativePathWithin(manifestRoot, absoluteSource); ok {
			manifestPath = relative
		}
	}

	return declarationcodec.SkillSource{
		Path: filepath.ToSlash(filepath.Clean(manifestPath)),
		Mode: mode,
	}, filepath.Base(absoluteSource), nil
}

func instructionSource(request AddInstructionRequest, manifestRoot string, effectiveScope string) (declarationcodec.InstructionSource, error) {
	if strings.HasPrefix(request.SourceArg, "s3://") {
		return declarationcodec.InstructionSource{}, fmt.Errorf("add instruction supports local and Git file sources; edit the manifest for S3 instruction sources")
	}
	gitInput := request.SourcePath != "" || request.Ref != "" || strings.Contains(request.SourceArg, "://")
	if !gitInput {
		if exists, err := pathExists(request.SourceArg); err == nil && exists {
			return localInstructionSource(request.SourceArg, manifestRoot, effectiveScope)
		}
		if locator, err := sourcepkg.ParseGitLocator(request.SourceArg); err == nil && !locator.IsNativeLocal() {
			gitInput = true
		}
	}
	if gitInput {
		source, err := gitSourceDeclaration(request.SourceArg, request.SourcePath, request.Ref)
		if err != nil {
			return declarationcodec.InstructionSource{}, err
		}
		if source.Path == "." {
			return declarationcodec.InstructionSource{}, fmt.Errorf("git instruction sources require a file path: use --path or owner/repo/path shorthand")
		}
		return declarationcodec.InstructionSource(source), nil
	}
	return localInstructionSource(request.SourceArg, manifestRoot, effectiveScope)
}

func localInstructionSource(sourceArg string, manifestRoot string, effectiveScope string) (declarationcodec.InstructionSource, error) {
	absoluteSource, err := filepath.Abs(sourceArg)
	if err != nil {
		return declarationcodec.InstructionSource{}, fmt.Errorf("resolve local source: %w", err)
	}
	manifestPath := absoluteSource
	if effectiveScope != string(target.ScopeGlobal) {
		if relative, ok := relativePathWithin(manifestRoot, absoluteSource); ok {
			manifestPath = relative
		}
	}
	return declarationcodec.InstructionSource{
		Path: filepath.ToSlash(filepath.Clean(manifestPath)),
		Mode: string(sourcepkg.LocalSourceModeVendor),
	}, nil
}

func skillGroupSource(request AddSkillGroupRequest, manifestRoot string) (declarationcodec.SkillSource, error) {
	if strings.TrimSpace(request.SourceArg) == "" {
		return declarationcodec.SkillSource{}, fmt.Errorf("missing skill_group source root")
	}
	if skillGroupSourceLooksGit(request.SourceArg, request.SourcePath, request.Ref) {
		return gitSkillGroupSource(request)
	}
	source, _, err := localSkillSource(AddSkillRequest{
		SourceArg:  request.SourceArg,
		SourcePath: request.SourcePath,
		Ref:        request.Ref,
		Scope:      request.Scope,
		Mode:       request.Mode,
	}, manifestRoot)
	if err != nil {
		return declarationcodec.SkillSource{}, err
	}
	return declarationcodec.SkillSource{
		Git:  source.Git,
		Path: source.Path,
		Ref:  source.Ref,
		Mode: source.Mode,
	}, nil
}

func skillGroupSourceLooksGit(source string, explicitPath string, ref string) bool {
	if explicitPath != "" || ref != "" {
		return true
	}
	if exists, err := pathExists(source); err == nil && exists {
		return false
	}
	if strings.Contains(source, "://") || strings.HasSuffix(source, ".git") {
		return true
	}
	if locator, err := sourcepkg.ParseGitLocator(source); err == nil {
		return !locator.IsNativeLocal()
	}
	owner, repo, _, ok := splitGitHubSourceShorthand(source)
	return ok && owner != "" && repo != ""
}

func gitSkillGroupSource(request AddSkillGroupRequest) (declarationcodec.SkillSource, error) {
	if request.Mode != "" && request.Mode != string(sourcepkg.LocalSourceModeVendor) {
		return declarationcodec.SkillSource{}, fmt.Errorf("--mode is only valid for local sources")
	}
	source, err := gitSourceDeclaration(request.SourceArg, request.SourcePath, request.Ref)
	return declarationcodec.SkillSource{Git: source.Git, Path: source.Path, Ref: source.Ref}, err
}

func gitSourceDeclaration(sourceArg string, sourcePath string, ref string) (declaration.Source, error) {
	if ref == "" {
		return declaration.Source{}, ErrMissingGitRef
	}
	gitURL := sourceArg
	if owner, repo, embeddedPath, ok := splitGitHubSourceShorthand(sourceArg); ok {
		if embeddedPath != "" && sourcePath != "" {
			return declaration.Source{}, fmt.Errorf("do not combine owner/repo/path shorthand with --path; use owner/repo --path %s", sourcePath)
		}
		gitURL = "https://github.com/" + owner + "/" + repo + ".git"
		if sourcePath == "" {
			sourcePath = embeddedPath
		}
	}
	if sourcePath == "" {
		sourcePath = "."
	}
	canonicalSource, err := sourcepkg.NewGitSource(gitURL, sourcePath, ref)
	if err != nil {
		return declaration.Source{}, err
	}
	gitSource, ok := canonicalSource.Git()
	if !ok {
		return declaration.Source{}, fmt.Errorf("canonical git source is unavailable")
	}
	return declaration.Source{
		Git:  gitSource.Locator().String(),
		Path: gitSource.RepositoryPath().String(),
		Ref:  gitSource.Ref().String(),
	}, nil
}

func skillSourceLooksGit(source string, explicitPath string, ref string) bool {
	if explicitPath != "" || ref != "" {
		return true
	}
	if exists, err := pathExists(source); err == nil && exists {
		return false
	}
	if strings.Contains(source, "://") || strings.HasSuffix(source, ".git") {
		return true
	}
	if locator, err := sourcepkg.ParseGitLocator(source); err == nil {
		return !locator.IsNativeLocal()
	}
	_, _, _, ok := splitGitHubSourceShorthand(source)
	return ok
}

func splitGitHubSourceShorthand(source string) (string, string, string, bool) {
	if _, err := sourcepkg.ParseGitLocator(source); err == nil {
		return "", "", "", false
	}
	if strings.HasPrefix(source, ".") || strings.HasPrefix(source, "~") || filepath.IsAbs(source) {
		return "", "", "", false
	}
	if strings.Contains(source, "://") || strings.HasPrefix(source, "git@") || strings.HasSuffix(source, ".git") {
		return "", "", "", false
	}
	parts := strings.Split(source, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], strings.Join(parts[2:], "/"), true
}

func gitRootSkillName(gitURL string) string {
	trimmedURL := strings.TrimRight(strings.TrimSpace(gitURL), "/")
	trimmedURL = strings.TrimSuffix(trimmedURL, ".git")
	return path.Base(filepath.ToSlash(trimmedURL))
}

func relativePathWithin(root string, candidate string) (string, bool) {
	relative, err := filepath.Rel(root, candidate)
	if err != nil || relative == "." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || relative == ".." {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
