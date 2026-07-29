package opencode

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
)

// HostLoadIdentity derives OpenCode's package or canonical local-file
// replacement identity without changing the exact stored source spelling.
func HostLoadIdentity(source string, sourcePath string) (string, error) {
	if err := validateSource(source); err != nil {
		return "", err
	}
	_, identity, err := pluginSourceIdentity(source, sourcePath)
	return identity, err
}

func pluginSourceIdentity(source string, sourcePath string) (string, string, error) {
	if pathLikePluginSource(source) {
		loadIdentity, err := canonicalPluginFileURL(source, sourcePath)
		if err != nil {
			return "", "", err
		}
		return source, loadIdentity, nil
	}
	if name, ok := npmPluginPackageName(source); ok {
		return source, name, nil
	}
	return source, source, nil
}

func pathLikePluginSource(source string) bool {
	return strings.HasPrefix(strings.ToLower(source), "file:") ||
		strings.HasPrefix(source, ".") ||
		filepath.IsAbs(source) ||
		(runtime.GOOS == "windows" && len(source) >= 3 && source[1] == ':')
}

func canonicalPluginFileURL(source string, sourcePath string) (string, error) {
	path := source
	if strings.HasPrefix(strings.ToLower(source), "file:") {
		parsed, err := url.Parse(source)
		if err != nil {
			return "", fmt.Errorf("parse OpenCode plugin file URL: %w", err)
		}
		if parsed.User != nil ||
			(parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost")) ||
			parsed.RawQuery != "" ||
			parsed.Fragment != "" {
			return "", fmt.Errorf("OpenCode plugin file URL has unsupported authority, query, or fragment")
		}
		path, err = url.PathUnescape(parsed.EscapedPath())
		if err != nil {
			return "", fmt.Errorf("decode OpenCode plugin file URL: %w", err)
		}
		if runtime.GOOS == "windows" &&
			len(path) >= 3 &&
			path[0] == '/' &&
			path[2] == ':' {
			path = path[1:]
		}
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		if sourcePath == "" {
			return source, nil
		}
		if !filepath.IsAbs(sourcePath) || filepath.Clean(sourcePath) != sourcePath {
			return "", fmt.Errorf("OpenCode config source path must be absolute and clean")
		}
		path = filepath.Join(filepath.Dir(sourcePath), path)
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("OpenCode plugin file source must resolve to an absolute path")
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String(), nil
}

func npmPluginPackageName(source string) (string, bool) {
	spec := source
	if strings.HasPrefix(spec, "npm:") {
		spec = strings.TrimPrefix(spec, "npm:")
	}
	if spec == "" {
		return "", false
	}

	name := spec
	if strings.HasPrefix(spec, "@") {
		slash := strings.IndexByte(spec, '/')
		if slash <= 1 {
			return "", false
		}
		if selector := strings.IndexByte(spec[slash+1:], '@'); selector >= 0 {
			name = spec[:slash+1+selector]
		}
	} else if selector := strings.IndexByte(spec, '@'); selector >= 0 {
		name = spec[:selector]
	}
	if !validNPMPackageName(name) {
		return "", false
	}
	return name, true
}

func validNPMPackageName(name string) bool {
	if name == "" ||
		strings.ContainsAny(name, `\`) ||
		strings.HasPrefix(name, ".") ||
		strings.HasSuffix(name, ".") {
		return false
	}
	validateSegment := func(segment string) bool {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, character := range segment {
			switch {
			case character >= 'a' && character <= 'z':
			case character >= 'A' && character <= 'Z':
			case character >= '0' && character <= '9':
			case character == '-', character == '_', character == '.':
			default:
				return false
			}
		}
		return true
	}
	if strings.HasPrefix(name, "@") {
		scope, packageName, ok := strings.Cut(strings.TrimPrefix(name, "@"), "/")
		return ok && validNPMPackageNameSegment(scope) && validateSegment(packageName)
	}
	return validateSegment(name)
}

func validNPMPackageNameSegment(segment string) bool {
	if segment == "" {
		return false
	}
	for _, character := range segment {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '-', character == '_', character == '.':
		default:
			return false
		}
	}
	return true
}
