package recover

import (
	"context"
	"errors"
	"fmt"
	"os"

	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/assurance/statefile"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/recoverygate"
)

func recoveryPaths(ctx context.Context, input PlanInput) (daempaths.Paths, error) {
	paths, err := daempaths.Resolve(input.ManifestPath)
	if err != nil || !input.LegacyUserState {
		return paths, err
	}
	legacy, err := paths.LegacyUserState()
	if err != nil {
		return daempaths.Paths{}, err
	}
	paths.LegacyUserStateDir = ""
	if err := recoverygate.Observe(ctx, paths); err != nil {
		return daempaths.Paths{}, fmt.Errorf("resolve canonical user-state recovery before legacy recovery: %w", err)
	}
	_, relocated, err := statefile.LoadRelocation(ctx, legacy.StatefilePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return daempaths.Paths{}, err
	}
	if relocated {
		return daempaths.Paths{}, fmt.Errorf("legacy user state has been relocated; use canonical recovery without --legacy-user-state")
	}
	return legacy, nil
}

func destinationResolver(paths daempaths.Paths) physicalhostpath.Resolver {
	return physicalhostpath.NewResolver(paths)
}
