package codexplugin

import (
	"context"
	"fmt"
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

func activePluginCacheVersion(
	ctx context.Context,
	cacheBase string,
	budget *observationBudget,
) (*pluginObservation, string, bool, bool, observecontribution.SourceContributionReason, error) {
	cache, reason, err := openPluginObservation(cacheBase, budget)
	if directoryMissing(err) {
		return nil, "", false, false, observecontribution.SourceContributionReasonNone, nil
	}
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, "", false, false, reason, err
	}

	names, reason, err := cache.listNames(ctx)
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		cache.close()
		return nil, "", false, false, reason, err
	}
	versions := make([]string, 0, len(names))
	for _, name := range names {
		if !validPluginSegment(name) {
			continue
		}
		kind, reason, err := cache.classify(name)
		if err != nil {
			cache.close()
			return nil, "", false, false, reason, err
		}
		if reason == observecontribution.SourceContributionReasonArtifactPathBlocked || kind == childSymlink {
			continue
		}
		if reason != observecontribution.SourceContributionReasonNone {
			cache.close()
			return nil, "", false, false, reason, err
		}
		if kind != childDirectory {
			continue
		}
		versions = append(versions, name)
	}
	sort.Strings(versions)
	selected := ""
	ambiguous := false
	for _, version := range versions {
		if version == "local" {
			selected = version
			break
		}
	}
	if selected == "" {
		switch len(versions) {
		case 0:
			cache.close()
			return nil, "", false, false, observecontribution.SourceContributionReasonNone, nil
		case 1:
			selected = versions[0]
		default:
			cache.close()
			return nil, "", false, true, observecontribution.SourceContributionReasonNone, nil
		}
	}

	plugin, reason, err := cache.openChildDirectory(selected)
	cache.close()
	if err != nil || reason != observecontribution.SourceContributionReasonNone {
		return nil, "", false, false, reason, err
	}
	return plugin, selected, true, ambiguous, observecontribution.SourceContributionReasonNone, nil
}

func cacheArtifactIdentity(id pluginID, version string) string {
	return filepath.ToSlash(filepath.Join("plugins", "cache", id.marketplace, id.plugin, version))
}
