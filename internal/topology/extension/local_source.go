package extension

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalSourceContext supplies stable, already-selected roots for resolving one
// carrier-local source without consulting process state or the filesystem.
type LocalSourceContext struct {
	baseRoot string
	homeRoot string
}

// NewLocalSourceContext validates the roots used to interpret relative and
// home-relative carrier sources.
func NewLocalSourceContext(baseRoot string, homeRoot string) (LocalSourceContext, error) {
	if err := validateLocalSourceRoot("carrier local source base", baseRoot); err != nil {
		return LocalSourceContext{}, err
	}
	if err := validateLocalSourceRoot("carrier local source home", homeRoot); err != nil {
		return LocalSourceContext{}, err
	}
	return LocalSourceContext{baseRoot: baseRoot, homeRoot: homeRoot}, nil
}

// LocalSourceIdentity is one lexical absolute carrier-local source identity.
// It deliberately does not resolve symlinks or require the source to exist.
type LocalSourceIdentity struct {
	path string
}

// Path returns the absolute clean lexical source path.
func (identity LocalSourceIdentity) Path() string { return identity.path }

// ResolveLocal resolves a local CarrierSource against explicit roots.
func (source CarrierSource) ResolveLocal(context LocalSourceContext) (LocalSourceIdentity, error) {
	if source.class != CarrierSourceLocal {
		return LocalSourceIdentity{}, fmt.Errorf(
			"carrier source class %q is not local",
			source.class,
		)
	}
	if err := validateLocalSourceRoot("carrier local source base", context.baseRoot); err != nil {
		return LocalSourceIdentity{}, err
	}
	if err := validateLocalSourceRoot("carrier local source home", context.homeRoot); err != nil {
		return LocalSourceIdentity{}, err
	}
	local, err := localSourcePath(source.identity)
	if err != nil {
		return LocalSourceIdentity{}, err
	}
	switch {
	case local == "~":
		local = context.homeRoot
	case strings.HasPrefix(local, "~"+string(filepath.Separator)):
		local = filepath.Join(
			context.homeRoot,
			strings.TrimPrefix(local, "~"+string(filepath.Separator)),
		)
	}
	if !filepath.IsAbs(local) {
		local = filepath.Join(context.baseRoot, local)
	}
	path := filepath.Clean(local)
	if err := validateLocalSourceRoot("carrier local source identity", path); err != nil {
		return LocalSourceIdentity{}, err
	}
	return LocalSourceIdentity{path: path}, nil
}

func localSourcePath(source string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(source), "file://") {
		return filepath.FromSlash(source), nil
	}
	parsed, err := url.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse carrier local file URL: %w", err)
	}
	if parsed.User != nil ||
		(parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost")) ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return "", fmt.Errorf(
			"carrier local file URL has unsupported authority, query, or fragment",
		)
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return "", fmt.Errorf("decode carrier local file URL path: %w", err)
	}
	if decodedPath == "" {
		return "", fmt.Errorf("carrier local file URL path is required")
	}
	if runtime.GOOS == "windows" &&
		len(decodedPath) >= 3 &&
		decodedPath[0] == '/' &&
		decodedPath[2] == ':' {
		decodedPath = decodedPath[1:]
	}
	return filepath.FromSlash(decodedPath), nil
}

func validateLocalSourceRoot(name string, value string) error {
	if value == "" || !filepath.IsAbs(value) || filepath.Clean(value) != value {
		return fmt.Errorf("%s %q must be absolute and clean", name, value)
	}
	return nil
}
