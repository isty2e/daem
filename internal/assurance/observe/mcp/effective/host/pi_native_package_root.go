package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
)

type piNativePackageContext struct {
	workDir    string
	agentRoot  string
	npmCommand json.RawMessage
}

func nativeAdapterPackageRoot(ctx context.Context, scope target.Scope, settings piNativePackageContext) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("Pi package-root observation requires a context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	root := settings.agentRoot
	switch scope {
	case target.ScopeProject:
		root = filepath.Join(settings.workDir, ".pi")
	case target.ScopeGlobal:
	default:
		return "", fmt.Errorf("unsupported Pi package installation scope %q", scope)
	}
	managed := filepath.Join(root, "npm", "node_modules", "pi-mcp-adapter")
	if scope == target.ScopeProject {
		return managed, nil
	}
	if _, err := os.Stat(managed); err == nil {
		return managed, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("Pi managed package-root existence cannot be observed")
	}

	legacy, err := nativeAdapterLegacyPackageRoot(ctx, settings)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(legacy); err == nil {
		return legacy, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("Pi legacy package-root existence cannot be observed")
	}
	return managed, nil
}

func nativeAdapterLegacyPackageRoot(ctx context.Context, settings piNativePackageContext) (string, error) {
	command := []string{"npm"}
	if len(settings.npmCommand) != 0 && string(settings.npmCommand) != "null" {
		configured, err := nativeSettingsStringArray(settings.npmCommand)
		if err != nil {
			return "", fmt.Errorf("Pi legacy package lookup requires a string-array npmCommand")
		}
		if len(configured) != 0 {
			command = configured
		}
	}
	if len(command) != 1 || strings.TrimSpace(command[0]) != command[0] || command[0] == "" || (!filepath.IsAbs(command[0]) && filepath.Base(command[0]) != command[0]) {
		return "", fmt.Errorf("Pi legacy package lookup does not execute wrappers or command prefixes")
	}
	manager := filepath.Base(command[0])
	suffix := filepath.Ext(manager)
	if strings.EqualFold(suffix, ".cmd") || strings.EqualFold(suffix, ".exe") {
		manager = manager[:len(manager)-len(suffix)]
	}
	if manager != "npm" && manager != "pnpm" && manager != "bun" {
		return "", fmt.Errorf("Pi legacy package lookup requires direct npm, pnpm or Bun")
	}

	if manager == "pnpm" {
		output, err := nativePackageLocationQuery(ctx, command[0], []string{"list", "-g", "--depth", "0", "--json"}, settings.workDir)
		if err != nil {
			return "", err
		}
		packageRoot, err := nativePnpmPackagePath(output)
		if err != nil || packageRoot != "" {
			return packageRoot, err
		}
	}
	args := []string{"root", "-g"}
	if manager == "bun" {
		args = []string{"pm", "bin", "-g"}
	}
	output, err := nativePackageLocationQuery(ctx, command[0], args, settings.workDir)
	if err != nil {
		return "", err
	}
	root, err := nativePackageLocationPath(output)
	if err != nil {
		return "", err
	}
	if manager == "bun" {
		root = filepath.Join(filepath.Dir(root), "install", "global", "node_modules")
	}
	return filepath.Join(root, "pi-mcp-adapter"), nil
}

func nativePackageLocationQuery(ctx context.Context, command string, args []string, workDir string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	result := subprocess.NewCommandExecutor(subprocess.CommandOptions{}).Execute(ctx, subprocess.CommandAttemptRequest{
		Command: command, Args: args, WorkDir: workDir, OutputLimit: int(maximumConfigBytes),
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !result.Succeeded() || !result.Started() || result.StdoutTruncated() || result.StderrTruncated() {
		return "", fmt.Errorf("Pi legacy package location query is unobservable (%s)", result.Reason())
	}
	return result.Stdout(), nil
}

func nativePackageLocationPath(output string) (string, error) {
	location := strings.TrimSpace(output)
	if location == "" || !filepath.IsAbs(location) || strings.ContainsFunc(location, unicode.IsControl) {
		return "", fmt.Errorf("Pi legacy package location must be an observable absolute native path")
	}
	return filepath.Clean(location), nil
}

func nativePnpmPackagePath(output string) (string, error) {
	if err := jsonstrict.Validate([]byte(output), "Pi pnpm global package listing", maximumConfigDepth); err != nil {
		return "", fmt.Errorf("Pi pnpm global package listing must be strict JSON")
	}
	var entries []json.RawMessage
	if json.Unmarshal([]byte(output), &entries) != nil || entries == nil {
		return "", fmt.Errorf("Pi pnpm global package listing must be an array")
	}
	for _, raw := range entries {
		var entry, dependencies, dependency map[string]json.RawMessage
		if json.Unmarshal(raw, &entry) != nil || entry == nil {
			return "", fmt.Errorf("Pi pnpm global package listing entries must be objects")
		}
		if raw, present := entry["dependencies"]; !present || string(raw) == "null" {
			continue
		}
		if json.Unmarshal(entry["dependencies"], &dependencies) != nil || dependencies == nil {
			return "", fmt.Errorf("Pi pnpm global package dependencies must be an object")
		}
		raw, present := dependencies["pi-mcp-adapter"]
		if !present || string(raw) == "null" {
			continue
		}
		if json.Unmarshal(raw, &dependency) != nil || dependency == nil {
			return "", fmt.Errorf("Pi pnpm global package entry must be an object")
		}
		raw, present = dependency["path"]
		if !present || string(raw) == "null" {
			continue
		}
		var location string
		if json.Unmarshal(raw, &location) != nil {
			return "", fmt.Errorf("Pi pnpm global package path must be a string")
		}
		if location != "" {
			return nativePackageLocationPath(location)
		}
	}
	return "", nil
}
