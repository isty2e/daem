package codexplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	observecontribution "github.com/isty2e/daem/internal/assurance/observe/contribution"
	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

type pluginID struct {
	plugin      string
	marketplace string
}

func parsePluginID(provider observecontribution.SourceProviderLabel) (pluginID, bool) {
	key := strings.TrimSpace(string(provider))
	plugin, marketplace, found := strings.Cut(key, "@")
	if !found || strings.Contains(marketplace, "@") {
		return pluginID{}, false
	}
	if !validPluginSegment(plugin) || !validPluginSegment(marketplace) {
		return pluginID{}, false
	}
	return pluginID{plugin: plugin, marketplace: marketplace}, true
}

func (id pluginID) providerLabel() observecontribution.SourceProviderLabel {
	return observecontribution.SourceProviderLabel(id.plugin + "@" + id.marketplace)
}

func (id pluginID) carrier() (extensiontopology.Carrier, error) {
	source, err := desiredextension.NewSourceRef(
		desiredextension.SourceKindMarketplace,
		string(id.providerLabel()),
	)
	if err != nil {
		return extensiontopology.Carrier{}, fmt.Errorf("Codex plugin provider source: %w", err)
	}
	key, err := desiredextension.NewCarrierKey(
		desiredextension.CarrierCodexPlugin,
		target.TargetCodex,
		target.ScopeGlobal,
		source,
	)
	if err != nil {
		return extensiontopology.Carrier{}, fmt.Errorf("Codex plugin provider identity: %w", err)
	}
	return extensiontopology.NewCarrier(key)
}

func validPluginSegment(value string) bool {
	if value == "." || value == ".." || !observecontribution.ValidSourceToken(value) {
		return false
	}
	for _, character := range value {
		if character == '/' || character == '\\' {
			return false
		}
	}
	return true
}

func activePluginCacheVersion(root string, cacheBase string) (string, bool, bool, observecontribution.SourceContributionReason) {
	if !pathWithin(root, cacheBase) || pathHasSymlinkComponent(root, cacheBase) {
		return "", false, false, observecontribution.SourceContributionReasonArtifactPathBlocked
	}
	entries, err := os.ReadDir(cacheBase)
	if err != nil {
		return "", false, false, observecontribution.SourceContributionReasonNone
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validPluginSegment(entry.Name()) {
			continue
		}
		versions = append(versions, entry.Name())
	}
	sort.Strings(versions)
	for _, version := range versions {
		if version == "local" {
			return version, true, false, observecontribution.SourceContributionReasonNone
		}
	}
	switch len(versions) {
	case 0:
		return "", false, false, observecontribution.SourceContributionReasonNone
	case 1:
		return versions[0], true, false, observecontribution.SourceContributionReasonNone
	default:
		return "", false, true, observecontribution.SourceContributionReasonNone
	}
}

func cacheArtifactIdentity(id pluginID, version string) string {
	return filepath.ToSlash(filepath.Join("plugins", "cache", id.marketplace, id.plugin, version))
}
