package host

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/encoding/jsonstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/target"
	"golang.org/x/mod/semver"
)

type piNativeAdapterPackage struct {
	source   string
	scope    target.Scope
	patterns []string
	filtered bool
	delta    bool
}

// Pi deduplicates npm package names independently of selectors. A project
// delta participates against the user installation rather than its own source.
func selectNativeAdapterPackages(global, project []piNativeAdapterPackage) []piNativeAdapterPackage {
	if len(project) == 0 {
		if len(global) == 0 {
			return nil
		}
		return global[:1]
	}
	selected := project[len(project)-1]
	if !selected.delta || len(global) == 0 {
		return []piNativeAdapterPackage{selected}
	}
	selected.source, selected.scope = global[0].source, global[0].scope
	return append([]piNativeAdapterPackage{selected}, global...)
}

func nativeAdapterResourcesSelected(ctx context.Context, packages []piNativeAdapterPackage, settings piNativePackageContext) (bool, error) {
	needsInventory := false
	for _, entry := range packages {
		needsInventory = needsInventory || len(entry.patterns) != 0
	}
	if !needsInventory {
		firstBase := -1
		uniformSource, anyDefault := true, false
		for index, entry := range packages {
			if entry.delta {
				continue
			}
			anyDefault = anyDefault || !entry.filtered
			if firstBase < 0 {
				firstBase = index
			} else if entry.source != packages[firstBase].source || entry.scope != packages[firstBase].scope {
				uniformSource = false
			}
		}
		if !anyDefault || firstBase < 0 {
			return false, nil
		}
		if !packages[firstBase].filtered || uniformSource {
			return !packages[firstBase].filtered, nil
		}
	}

	resources, version, err := observeNativeAdapterInventory(ctx, packages[0], settings)
	if err != nil {
		return false, err
	}
	unicodeInventory := false
	for _, resource := range resources {
		unicodeInventory = unicodeInventory || utf8.RuneCountInString(resource) != len(resource)
	}
	patterns := make([][]nativeResourcePattern, len(packages))
	for index, entry := range packages {
		if !nativeAdapterInventoryVersionMatches(entry.source, version) {
			return false, fmt.Errorf("native Pi MCP package inventory does not match its selected npm version")
		}
		for _, directive := range entry.patterns {
			pattern, err := newNativeResourcePattern(directive)
			if err != nil {
				return false, err
			}
			if !pattern.exact() && strings.ContainsAny(pattern.value, "*?") && (unicodeInventory || utf8.RuneCountInString(pattern.value) != len(pattern.value)) {
				return false, fmt.Errorf("native Pi MCP Unicode package glob matching is outside the static selector envelope")
			}
			patterns[index] = append(patterns[index], pattern)
		}
	}
	for _, resource := range resources {
		for index, entry := range packages {
			decision := entry.resourceDecision(resource, patterns[index])
			if decision == nativeResourceEnabled {
				return true, nil
			}
			if decision == nativeResourceDisabled {
				break
			}
		}
	}
	return false, nil
}

func observeNativeAdapterInventory(ctx context.Context, entry piNativeAdapterPackage, settings piNativePackageContext) ([]string, string, error) {
	packageRoot, err := nativeAdapterPackageRoot(ctx, entry.scope, settings)
	if err != nil {
		return nil, "", err
	}
	content, exists, err := filesnapshot.ReadRegularFile(filepath.Join(packageRoot, "package.json"), maximumConfigBytes)
	if err != nil || !exists || jsonstrict.Validate(content, "Pi adapter package metadata", maximumConfigDepth) != nil {
		return nil, "", fmt.Errorf("native Pi MCP package filters require observable strict installed package metadata")
	}
	var metadata, manifest map[string]json.RawMessage
	var name, version string
	if json.Unmarshal(content, &metadata) != nil || metadata == nil ||
		json.Unmarshal(metadata["name"], &name) != nil || name != "pi-mcp-adapter" ||
		json.Unmarshal(metadata["version"], &version) != nil ||
		json.Unmarshal(metadata["pi"], &manifest) != nil || manifest == nil {
		return nil, "", fmt.Errorf("native Pi MCP package filters require a finite explicit extension inventory")
	}
	extensions, err := nativeSettingsStringArray(manifest["extensions"])
	if err != nil || len(extensions) == 0 {
		return nil, "", fmt.Errorf("native Pi MCP package filters require a finite explicit extension inventory")
	}
	resources := make([]string, 0, len(extensions))
	seen := make(map[string]struct{}, len(extensions))
	for _, resource := range extensions {
		if resource != "" && strings.ContainsRune("!+-", rune(resource[0])) {
			return nil, "", fmt.Errorf("native Pi MCP package filters cannot qualify manifest override inventories")
		}
		resource = strings.TrimPrefix(resource, "./")
		if resource == "" || path.IsAbs(resource) || strings.HasPrefix(resource, "../") || path.Clean(resource) != resource || strings.ContainsAny(resource, `\*?[]{}()!:`) || (path.Ext(resource) != ".js" && path.Ext(resource) != ".ts") {
			return nil, "", fmt.Errorf("native Pi MCP package filters cannot qualify dynamic or aliased extension inventories")
		}
		if _, duplicate := seen[resource]; duplicate {
			continue
		}
		seen[resource] = struct{}{}
		info, err := os.Lstat(filepath.Join(packageRoot, filepath.FromSlash(resource)))
		if (err != nil && !os.IsNotExist(err)) || (err == nil && !info.Mode().IsRegular()) {
			return nil, "", fmt.Errorf("native Pi MCP package filters require regular extension file inventory entries")
		}
		resources = append(resources, resource)
	}
	return resources, version, nil
}

func nativeAdapterInventoryVersionMatches(source, version string) bool {
	const prefix = "npm:pi-mcp-adapter@"
	if !strings.HasPrefix(source, prefix) {
		return false
	}
	selector := strings.TrimPrefix(source, prefix)
	caret := strings.HasPrefix(selector, "^")
	wanted := "v" + strings.TrimPrefix(selector, "^")
	current := "v" + version
	for _, value := range []string{wanted, current} {
		if !semver.IsValid(value) || semver.Canonical(value) != value || semver.Prerelease(value) != "" || semver.Build(value) != "" {
			return false
		}
	}
	if !caret {
		return wanted == current
	}
	if semver.Major(wanted) != semver.Major(current) || semver.Compare(current, wanted) < 0 {
		return false
	}
	if semver.Major(wanted) == "v0" {
		if semver.MajorMinor(wanted) == "v0.0" {
			return current == wanted
		}
		return semver.MajorMinor(wanted) == semver.MajorMinor(current)
	}
	return true
}

type nativeResourceAction uint8

const (
	nativeResourceInclude nativeResourceAction = iota
	nativeResourceExclude
	nativeResourceForceInclude
	nativeResourceForceExclude
)

type nativeResourcePattern struct {
	action nativeResourceAction
	value  string
}

// Full Minimatch parity is outside the static selector envelope in docs/host-integrations.md.
func newNativeResourcePattern(directive string) (nativeResourcePattern, error) {
	pattern := nativeResourcePattern{value: filepath.ToSlash(directive)}
	if strings.ContainsRune(pattern.value, '\\') {
		return nativeResourcePattern{}, fmt.Errorf("native Pi MCP package filter escapes are outside the static selector envelope")
	}
	if pattern.value != "" {
		switch pattern.value[0] {
		case '!':
			pattern.action = nativeResourceExclude
		case '+':
			pattern.action = nativeResourceForceInclude
		case '-':
			pattern.action = nativeResourceForceExclude
		}
		if pattern.action != nativeResourceInclude {
			pattern.value = pattern.value[1:]
		}
	}
	if pattern.exact() {
		pattern.value = strings.TrimPrefix(pattern.value, "./")
	}
	if path.IsAbs(pattern.value) || strings.ContainsAny(pattern.value, "[]{}()!:") || strings.Contains(pattern.value, "**") || strings.Contains(pattern.value, "../") {
		return nativeResourcePattern{}, fmt.Errorf("native Pi MCP package filter is outside the static relative selector envelope")
	}
	if !pattern.exact() && (strings.HasPrefix(pattern.value, "#") || strings.Contains(pattern.value, "//")) {
		return nativeResourcePattern{}, fmt.Errorf("native Pi MCP package filter aliases or comments are outside the static selector envelope")
	}
	return pattern, nil
}

func (pattern nativeResourcePattern) exact() bool {
	return pattern.action == nativeResourceForceInclude || pattern.action == nativeResourceForceExclude
}

func (pattern nativeResourcePattern) matches(resource string) bool {
	if pattern.exact() {
		return pattern.value == resource
	}
	for _, candidate := range []string{resource, path.Base(resource)} {
		matched, err := path.Match(pattern.value, candidate)
		if err != nil || !matched {
			continue
		}
		candidateParts, patternParts := strings.Split(candidate, "/"), strings.Split(pattern.value, "/")
		for index, part := range candidateParts {
			if strings.HasPrefix(part, ".") && !strings.HasPrefix(patternParts[index], ".") {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

type nativeResourceDecision uint8

const (
	nativeResourceUndecided nativeResourceDecision = iota
	nativeResourceDisabled
	nativeResourceEnabled
)

func (entry piNativeAdapterPackage) resourceDecision(resource string, patterns []nativeResourcePattern) nativeResourceDecision {
	if entry.delta {
		decision := nativeResourceUndecided
		for _, pattern := range patterns {
			if pattern.matches(resource) {
				decision = nativeResourceEnabled
				if pattern.action == nativeResourceExclude || pattern.action == nativeResourceForceExclude {
					decision = nativeResourceDisabled
				}
			}
		}
		return decision
	}
	if entry.filtered && len(patterns) == 0 {
		return nativeResourceDisabled
	}
	var matched [4]bool
	plain := false
	for _, pattern := range patterns {
		plain = plain || pattern.action == nativeResourceInclude
		matched[pattern.action] = matched[pattern.action] || pattern.matches(resource)
	}
	enabled := (!plain || matched[nativeResourceInclude]) && !matched[nativeResourceExclude]
	enabled = (enabled || matched[nativeResourceForceInclude]) && !matched[nativeResourceForceExclude]
	if enabled {
		return nativeResourceEnabled
	}
	return nativeResourceDisabled
}
