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
)

func observeNativeLocalPackageName(ctx context.Context, source, settingsBase string) (string, error) {
	for _, prefix := range []string{"npm:", "git:", "http:", "https:", "ssh:", "builtin:"} {
		if strings.HasPrefix(source, prefix) {
			return "", nil
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	location, err := nativeLocalPackagePath(source, settingsBase)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(location)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("native Pi MCP local package location cannot be observed")
	}
	if info.Mode().IsRegular() {
		location = filepath.Dir(location)
	} else if !info.IsDir() {
		return "", nil
	}

	content, exists, err := filesnapshot.ReadRegularFileContext(ctx, filepath.Join(location, "package.json"), maximumConfigBytes)
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
