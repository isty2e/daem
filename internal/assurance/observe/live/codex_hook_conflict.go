package live

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/isty2e/daem/internal/encoding/toml"
	tomlfields "github.com/isty2e/daem/internal/encoding/toml/fields"
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
	if err := tomlsyntax.Admit(ctx, content, tomlsyntax.StandardLimits()); err != nil {
		return fmt.Errorf(
			"observe destination %q: parse %q for Codex inline hooks: %w",
			destination,
			configDestination,
			err,
		)
	}

	var decoded map[string]any
	metadata, err := tomlfields.DecodeAdmitted(ctx, content, &decoded)
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
