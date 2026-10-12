package host

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/realization/profile"
)

func observeNativeLocalPackageNames(ctx context.Context, source, settingsBase string) ([]string, error) {
	if !profile.PiMCPPackageSourceIsLocal(source) {
		return nil, nil
	}
	return observeNativeLocalPathPackageNames(ctx, source, settingsBase)
}

func observeNativeLocalPathPackageNames(ctx context.Context, source, settingsBase string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	location, err := nativeLocalPackagePath(source, settingsBase)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(location)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("native Pi MCP local package location cannot be observed")
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		return nil, nil
	}
	resolved, err := filepath.EvalSymlinks(location)
	if err != nil {
		return nil, fmt.Errorf("native Pi MCP local package target cannot be resolved")
	}

	metadataDirectories := []string{resolved}
	if info.Mode().IsRegular() {
		configuredParent, err := filepath.EvalSymlinks(filepath.Dir(location))
		if err != nil {
			return nil, fmt.Errorf("native Pi MCP local package parent cannot be resolved")
		}
		metadataDirectories = []string{configuredParent}
		if targetParent := filepath.Dir(resolved); targetParent != configuredParent {
			metadataDirectories = append(metadataDirectories, targetParent)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	names := make([]string, 0, len(metadataDirectories))
	for _, directory := range metadataDirectories {
		name, err := readNativeLocalPackageName(ctx, directory)
		if err != nil {
			return nil, err
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func readNativeLocalPackageName(ctx context.Context, directory string) (string, error) {
	content, exists, err := filesnapshot.ReadRegularFileContext(ctx, filepath.Join(directory, "package.json"), maximumConfigBytes)
	if err != nil {
		return "", fmt.Errorf("native Pi MCP local package metadata cannot be read as a bounded regular file")
	}
	if !exists {
		return "", nil
	}
	if jsonstrict.Validate(content, "Pi local package metadata", maximumConfigDepth) != nil {
		return "", fmt.Errorf("native Pi MCP local package metadata requires strict unambiguous JSON")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(content, &fields) != nil || fields == nil {
		return "", fmt.Errorf("native Pi MCP local package metadata must be an object")
	}
	if raw, present := fields["name"]; present {
		var name string
		if string(raw) == "null" || json.Unmarshal(raw, &name) != nil {
			return "", fmt.Errorf("native Pi MCP local package name must be a string")
		}
		return name, nil
	}
	return "", nil
}

func nativeLocalPackagePath(source, settingsBase string) (string, error) {
	source = strings.TrimSpace(source)

	if strings.HasPrefix(source, "file://") {
		parsed, err := url.Parse(source)
		if err != nil || (parsed.Host != "" && parsed.Host != "localhost") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
			return "", fmt.Errorf("native Pi MCP local package file URL is outside the canonical local envelope")
		}
		source = filepath.FromSlash(parsed.Path)
		if filepath.Separator == '\\' && strings.HasPrefix(source, `\`) && filepath.VolumeName(source[1:]) != "" {
			source = source[1:]
		}
		if !filepath.IsAbs(source) {
			return "", fmt.Errorf("native Pi MCP local package file URL must be an absolute native path")
		}
	}
	if source == "~" || strings.HasPrefix(source, "~/") || (filepath.Separator == '\\' && strings.HasPrefix(source, `~\`)) {
		home, err := os.UserHomeDir()
		if err != nil || !filepath.IsAbs(home) {
			return "", fmt.Errorf("native Pi MCP local package tilde source requires an observable home")
		}
		if source == "~" {
			source = home
		} else {
			source = filepath.Join(home, source[2:])
		}
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(settingsBase, source)
	}
	return filepath.Clean(source), nil
}
