package authoring

import (
	"context"
	"fmt"

	"github.com/isty2e/daem/internal/declarationartifact"
	"github.com/isty2e/daem/internal/desired/hostconfig"
	"github.com/isty2e/daem/internal/effect/mutation"
)

func rejectHostConfigControlSourceOverlap(ctx context.Context, configs []hostconfig.Config, root string, manifest, lockfile declarationartifact.MutationTarget) error {
	var controls []mutation.DirectoryEntryAuthorityObservation
	for _, config := range configs {
		local, ok := config.Source().Local()
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if controls == nil {
			for _, target := range []declarationartifact.MutationTarget{manifest, lockfile} {
				entry, err := mutation.ObserveDirectoryEntryAuthority(target.Path())
				if err != nil {
					return err
				}
				controls = append(controls, entry)
			}
		}

		referent, _, err := mutation.CanonicalReferentPath(localHostConfigFile(root, local))
		if err != nil {
			return fmt.Errorf("locate host configuration %q source: %w", config.ID().Name(), err)
		}
		entry, err := mutation.ObserveDirectoryEntryAuthority(referent)
		if err != nil {
			return fmt.Errorf("observe host configuration %q source entry: %w", config.ID().Name(), err)
		}
		for _, control := range controls {
			if control.Equal(entry) {
				return fmt.Errorf("host configuration %q source is the manifest or lockfile selected for replacement; use a separate source file", config.ID().Name())
			}
		}
	}
	return nil
}
