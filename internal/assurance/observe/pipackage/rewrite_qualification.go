package pipackage

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/output"
	piconfig "github.com/isty2e/daem/internal/realization/configrelation/pi"
	"github.com/isty2e/daem/internal/target"
	extensiontopology "github.com/isty2e/daem/internal/topology/extension"
)

// PreservesOutsidePackageRewrite compares native values outside complete rows
// sharing the selected source's canonical host-load identity. It supplies neither
// desired-membership qualification, selected-key diagnostics nor write authority.
func PreservesOutsidePackageRewrite(
	ctx context.Context,
	before, after configdocument.Document,
	selectedSource string,
	scope target.Scope,
	sourceContext extensiontopology.LocalSourceContext,
) (bool, error) {
	if ctx == nil {
		return false, fmt.Errorf("Pi package rewrite context is required")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	selectedIdentity, err := HostLoadIdentityForContext(selectedSource, scope, sourceContext)
	if err != nil {
		return false, fmt.Errorf("selected Pi package source: %w", err)
	}
	path, err := output.NewKeyPath("packages")
	if err != nil {
		return false, err
	}
	beforeRegion, err := packageRewriteRegion(ctx, before, path, selectedIdentity, scope, sourceContext)
	if err != nil {
		return false, fmt.Errorf("before Pi package rewrite: %w", err)
	}
	afterRegion, err := packageRewriteRegion(ctx, after, path, selectedIdentity, scope, sourceContext)
	if err != nil {
		return false, fmt.Errorf("after Pi package rewrite: %w", err)
	}
	return before.PreservesOutside(ctx, after,
		[]configdocument.RewriteRegion{beforeRegion},
		[]configdocument.RewriteRegion{afterRegion})
}

func packageRewriteRegion(
	ctx context.Context,
	document configdocument.Document,
	path output.KeyPath,
	selectedIdentity string,
	scope target.Scope,
	sourceContext extensiontopology.LocalSourceContext,
) (configdocument.RewriteRegion, error) {
	var indices []int
	if document.Exists() {
		parsed, err := piconfig.Parse(document.Bytes())
		if err != nil {
			return configdocument.RewriteRegion{}, err
		}
		for index, entry := range parsed.Entries() {
			if err := ctx.Err(); err != nil {
				return configdocument.RewriteRegion{}, err
			}
			identity, err := HostLoadIdentityForContext(entry.Source(), scope, sourceContext)
			if err != nil {
				return configdocument.RewriteRegion{}, fmt.Errorf("Pi package source[%d]: %w", index, err)
			}
			if identity == selectedIdentity {
				indices = append(indices, index)
			}
		}
	}
	return configdocument.JSONArrayRegion(path, indices...)
}
