// Package native resolves versioned native-root contracts from one captured
// host context. It does not open locations or grant filesystem authority.
package native

import (
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
)

var locationVariables = [...]string{
	"PI_CODING_AGENT_DIR", "CODEX_HOME", "CLAUDE_CONFIG_DIR", "OPENCODE_CONFIG_DIR",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"OPENCODE_DB", "OPENCODE_PLUGIN_META_FILE",
}

type Locations struct {
	workDir string
	home    string
	values  map[string]string
}

func CaptureLocations(workDir string) (Locations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Locations{}, fmt.Errorf("native root home could not be selected: %w", err)
	}
	values := make(map[string]string, len(locationVariables))
	for _, name := range locationVariables {
		if value, present := os.LookupEnv(name); present {
			values[name] = value
		}
	}
	return NewLocations(workDir, home, values)
}

func NewLocations(workDir, home string, values map[string]string) (Locations, error) {
	for _, value := range []string{workDir, home} {
		if !filepath.IsAbs(value) || strings.ContainsRune(value, '\x00') {
			return Locations{}, fmt.Errorf("native location context requires absolute working and home directories without a NUL byte")
		}
	}
	return Locations{workDir: workDir, home: home, values: maps.Clone(values)}, nil
}

// Location is a literal selector and a non-secret description of its source.
// Source never contains the value of an environment variable.
type Location struct {
	path   string
	source string
}

func (location Location) Path() string   { return location.path }
func (location Location) Source() string { return location.source }

func (locations Locations) Resolve(reference output.RootReference) (Location, error) {
	if locations.workDir == "" || locations.home == "" {
		return Location{}, fmt.Errorf("native location context is not initialized")
	}
	contracts, err := configuration.NativeRootContracts()
	if err != nil {
		return Location{}, err
	}
	known := false
	for _, contract := range contracts {
		known = known || contract.Reference() == reference
	}
	if !known {
		return Location{}, fmt.Errorf("native root reference has no supported locator contract")
	}
	var result Location
	switch reference.LocatorPolicy() {
	case "pi-agent-location-v1":
		result = locations.selected("PI_CODING_AGENT_DIR", filepath.Join(locations.home, ".pi", "agent"))
		result.path, err = PiAgentDirectory(locations.values["PI_CODING_AGENT_DIR"], locations.workDir, locations.home)
	case "codex-home-location-v1":
		result = locations.selected("CODEX_HOME", filepath.Join(locations.home, ".codex"))
		result.path, err = CodexHomeDirectory(locations.values["CODEX_HOME"], locations.workDir, locations.home)
	case "claude-preferences-location-v1":
		result = locations.selected("CLAUDE_CONFIG_DIR", filepath.Join(locations.home, ".claude"))
		result.path = locations.absolute(result.path)
	case "claude-account-location-v1":
		result = locations.selected("CLAUDE_CONFIG_DIR", locations.home)
		result.path = locations.absolute(result.path)
	case "opencode-location-v1":
		if locations.values["OPENCODE_CONFIG_DIR"] != "" {
			result = locations.selected("OPENCODE_CONFIG_DIR", "")
			result.path = locations.absolute(result.path)
			break
		}
		base := locations.selected("XDG_CONFIG_HOME", filepath.Join(locations.home, ".config"))
		if !filepath.IsAbs(base.path) {
			return Location{source: base.source}, fmt.Errorf("OpenCode configuration base must be absolute")
		}
		result = Location{path: filepath.Join(base.path, "opencode"), source: base.source}
	case "antigravity-preferences-location-v1":
		result = Location{path: filepath.Join(locations.home, ".gemini", "antigravity-cli"), source: "default location"}
	case "antigravity-shared-location-v1":
		result = Location{path: filepath.Join(locations.home, ".gemini", "config"), source: "default location"}
	default:
		return Location{}, fmt.Errorf("native root locator contract is not implemented")
	}
	if err != nil {
		return result, err
	}
	if result.path == "" || !filepath.IsAbs(result.path) || strings.ContainsRune(result.path, '\x00') {
		return result, fmt.Errorf("native root selected by %s is not an admitted absolute locator", result.source)
	}
	return result, nil
}

func (locations Locations) selected(variable, fallback string) Location {
	if configured := locations.values[variable]; configured != "" {
		return Location{path: configured, source: variable}
	}
	return Location{path: fallback, source: "default location"}
}

func (locations Locations) absolute(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	// Do not Join: a literal parent following a symlink belongs to the locator's
	// physical interpretation, not lexical path cleaning.
	return locations.workDir + string(filepath.Separator) + path
}

// PiAgentDirectory implements Pi's lexical Node-path contract without reading
// the process environment or resolving filesystem aliases.
func PiAgentDirectory(configured, workDir, home string) (string, error) {
	path := configured
	if path == "" {
		if err := validateLocatorBase(home); err != nil {
			return "", fmt.Errorf("Pi default home: %w", err)
		}
		path = filepath.Join(home, ".pi", "agent")
	}
	if runtime.GOOS == "windows" {
		path = normalizePiWindowsShellPath(path)
	}
	if path == "~" || strings.HasPrefix(path, "~/") || runtime.GOOS == "windows" && strings.HasPrefix(path, `~\`) {
		if err := validateLocatorBase(home); err != nil {
			return "", fmt.Errorf("Pi tilde home: %w", err)
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, path[2:])
		}
	} else if strings.HasPrefix(path, "file://") {
		var err error
		path, err = piFileURLPath(path)
		if err != nil {
			return "", err
		}
	}
	if strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("Pi agent locator must not contain a NUL byte")
	}
	if !filepath.IsAbs(path) {
		if err := validateLocatorBase(workDir); err != nil {
			return "", fmt.Errorf("Pi working directory: %w", err)
		}
		path = filepath.Join(workDir, path)
	}
	return filepath.Clean(path), nil
}

// CodexHomeDirectory preserves literal parent components until the bounded
// filesystem boundary resolves the configured selector.
func CodexHomeDirectory(configured, workDir, home string) (string, error) {
	if configured == "" {
		if err := validateLocatorBase(home); err != nil {
			return "", fmt.Errorf("Codex default home: %w", err)
		}
		return filepath.Join(home, ".codex"), nil
	}
	if strings.ContainsRune(configured, '\x00') {
		return "", fmt.Errorf("Codex home locator must not contain a NUL byte")
	}
	if filepath.IsAbs(configured) {
		return configured, nil
	}
	if err := validateLocatorBase(workDir); err != nil {
		return "", fmt.Errorf("Codex working directory: %w", err)
	}
	return workDir + string(filepath.Separator) + configured, nil
}

func validateLocatorBase(path string) error {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
		return fmt.Errorf("locator context requires an absolute directory without a NUL byte")
	}
	return nil
}

func normalizePiWindowsShellPath(path string) string {
	if strings.HasPrefix(path, "//") || strings.Contains(path, `\`) {
		return path
	}
	for _, prefix := range []string{"/cygdrive/", "/mnt/", "/"} {
		if !strings.HasPrefix(path, prefix) {
			continue
		}
		rest := strings.TrimPrefix(path, prefix)
		if len(rest) == 0 || len(rest) > 1 && rest[1] != '/' {
			continue
		}
		letter := rest[0]
		if !(letter >= 'A' && letter <= 'Z' || letter >= 'a' && letter <= 'z') {
			continue
		}
		return strings.ToUpper(rest[:1]) + `:\` + strings.ReplaceAll(strings.TrimPrefix(rest[1:], "/"), "/", `\`)
	}
	return path
}

func piFileURLPath(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "file" || parsed.User != nil || strings.Contains(parsed.Host, ":") || parsed.Opaque != "" {
		return "", fmt.Errorf("Pi agent file URL is invalid")
	}
	escaped := strings.ToLower(parsed.EscapedPath())
	if strings.Contains(escaped, "%2f") || runtime.GOOS == "windows" && strings.Contains(escaped, "%5c") {
		return "", fmt.Errorf("Pi agent file URL contains an encoded path separator")
	}
	path := parsed.Path
	if path == "" {
		path = "/"
	}
	if runtime.GOOS == "windows" {
		if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
			return `\\` + parsed.Host + filepath.FromSlash(path), nil
		}
		if len(path) < 3 || path[0] != '/' || path[2] != ':' {
			return "", fmt.Errorf("Pi agent file URL requires an absolute drive path")
		}
		return filepath.FromSlash(path[1:]), nil
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("Pi agent file URL has an unsupported host")
	}
	return path, nil
}
