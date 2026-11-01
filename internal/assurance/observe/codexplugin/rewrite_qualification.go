package codexplugin

import (
	"context"
	"fmt"

	desiredextension "github.com/isty2e/daem/internal/desired/extension"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
)

// ConfigDocumentAddress projects the Codex-owned user config into native document identity.
func ConfigDocumentAddress() (aggregate.DocumentAddress, error) {
	destination, err := output.Parse("@codex-home/config.toml")
	if err != nil {
		return aggregate.DocumentAddress{}, err
	}
	return aggregate.NewDocumentAddress(target.TargetCodex, target.ScopeGlobal, destination)
}

// PreservesOutsidePluginRewrite compares values and comments outside the selected
// plugin entry. It does not qualify membership or grant write authority.
func PreservesOutsidePluginRewrite(ctx context.Context, before, after configdocument.Document, source desiredextension.SourceRef) (bool, error) {
	selector, ok := source.MarketplaceSelector()
	if !ok {
		return false, fmt.Errorf("Codex plugin rewrite requires a marketplace selector")
	}
	return preservesOutsideEntryRewrite(ctx, before, after, "plugins", selector.String())
}

// PreservesOutsideMarketplaceRewrite compares values and comments outside the
// selected marketplace entry, without admitting changes to its sibling plugins.
func PreservesOutsideMarketplaceRewrite(ctx context.Context, before, after configdocument.Document, source desiredextension.SourceRef) (bool, error) {
	selector, ok := source.MarketplaceSelector()
	if !ok {
		return false, fmt.Errorf("Codex marketplace rewrite requires a marketplace selector")
	}
	return preservesOutsideEntryRewrite(ctx, before, after, "marketplaces", selector.Marketplace())
}

func preservesOutsideEntryRewrite(ctx context.Context, before, after configdocument.Document, container, entry string) (bool, error) {
	path, err := output.NewKeyPath(container, entry)
	if err != nil {
		return false, err
	}
	region, err := configdocument.SubtreeRegion(path)
	if err != nil {
		return false, err
	}
	return before.PreservesOutside(ctx, after, []configdocument.RewriteRegion{region}, []configdocument.RewriteRegion{region})
}
