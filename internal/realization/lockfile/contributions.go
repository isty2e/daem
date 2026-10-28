package lockfile

import (
	"context"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
)

func currentContributions(ctx context.Context) (aggregate.ContributionCatalog, error) {
	var required []aggregate.CodecContractID
	for _, placement := range aggregate.ImplementedHookPlacements() {
		required = append(required, placement.CodecContractID())
	}
	for _, placement := range aggregate.ImplementedMCPContracts() {
		required = append(required, placement.CodecContractID())
	}
	for _, syntax := range []string{"json", "jsonc", "json-line-comments", "toml"} {
		id, _ := aggregate.HostConfigCodecForSyntax(syntax)
		required = append(required, id)
	}
	return aggregatecodec.ForContributions(ctx, required, configdocument.Limits{
		MaximumBytes: declarationartifact.MaximumBytes,
		MaximumDepth: tomlstrict.MaximumDepth,
	})
}
