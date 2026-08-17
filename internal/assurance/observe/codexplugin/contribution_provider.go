package codexplugin

import (
	"context"
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

func activePluginCacheVersion(
	ctx context.Context,
	root string,
	cacheBase string,
	budget *observationBudget,
) (string, bool, bool, observecontribution.SourceContributionReason, error) {
	names, reason, err := listContainedDirectoryNames(ctx, root, cacheBase, budget)
	if err != nil {
		if directoryMissing(err) {
			return "", false, false, observecontribution.SourceContributionReasonNone, nil
		}
		if observationCanceled(err) {
			return "", false, false, observecontribution.SourceContributionReasonNone, err
		}
		return "", false, false, observecontribution.SourceContributionReasonNone, nil
	}
	if reason != observecontribution.SourceContributionReasonNone {
		return "", false, false, reason, nil
	}
	versions := make([]string, 0, len(names))
	for _, name := range names {
		if !validPluginSegment(name) {
			continue
		}
		info, statErr := os.Lstat(filepath.Join(cacheBase, name))
		if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		versions = append(versions, name)
	}
	sort.Strings(versions)
	for _, version := range versions {
		if version == "local" {
			return version, true, false, observecontribution.SourceContributionReasonNone, nil
		}
	}
	switch len(versions) {
	case 0:
		return "", false, false, observecontribution.SourceContributionReasonNone, nil
	case 1:
		return versions[0], true, false, observecontribution.SourceContributionReasonNone, nil
	default:
		return "", false, true, observecontribution.SourceContributionReasonNone, nil
	}
}

func cacheArtifactIdentity(id pluginID, version string) string {
	return filepath.ToSlash(filepath.Join("plugins", "cache", id.marketplace, id.plugin, version))
}
