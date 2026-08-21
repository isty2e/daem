package live

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/encoding/tomlstrict"
	"github.com/isty2e/daem/internal/filesnapshot"
	"github.com/isty2e/daem/internal/output"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
)

const maximumCodexInlineConfigBytes int64 = 4 << 20

// ValidateAggregateReadPreconditions checks host-boundary facts that live
// outside an aggregate document before its codec is allowed to observe it.
func ValidateAggregateReadPreconditions(
	ctx context.Context,
	destination output.Destination,
	resolver DestinationResolver,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// This is a private Codex boundary guard, not a generic hook policy.
	configDestination, ok := commandhook.CodexInlineConfigDestination(destination)
	if !ok {
		return nil
	}

	configPath, err := resolver(configDestination)
	if err != nil {
		return err
	}
	content, exists, err := filesnapshot.ReadRegularFileReferentContext(
		ctx,
		configPath,
		maximumCodexInlineConfigBytes,
	)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf(
			"observe destination %q: parse %q for Codex inline hooks: %w",
			destination,
			configDestination,
			err,
		)
	}
	if !exists {
		return nil
	}
	if err := tomlstrict.Admit(ctx, content, tomlstrict.StandardLimits()); err != nil {
		return fmt.Errorf(
			"observe destination %q: parse %q for Codex inline hooks: %w",
			destination,
			configDestination,
			err,
		)
	}

	var decoded map[string]toml.Primitive
	metadata, err := tomlstrict.DecodeAdmitted(ctx, content, &decoded)
	if err != nil {
		return fmt.Errorf(
			"observe destination %q: parse %q for Codex inline hooks: %w",
			destination,
			configDestination,
			err,
		)
	}
	if metadata.IsDefined("hooks") {
		return fmt.Errorf("observe destination %q: unmanaged Codex inline hooks found in %q", destination, configDestination)
	}

	return nil
}
