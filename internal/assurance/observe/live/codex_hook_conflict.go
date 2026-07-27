package live

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/isty2e/daem/internal/output"
	commandhook "github.com/isty2e/daem/internal/realization/aggregate/hook"
)

// ValidateAggregateReadPreconditions checks host-boundary facts that live
// outside an aggregate document before its codec is allowed to observe it.
func ValidateAggregateReadPreconditions(destination output.Destination, resolver DestinationResolver) error {
	// This is a private Codex boundary guard, not a generic hook policy.
	configDestinationValue, ok := commandhook.CodexInlineConfigDestination(destination.String())
	if !ok {
		return nil
	}
	configDestination, err := output.Parse(configDestinationValue)
	if err != nil {
		return fmt.Errorf("Codex inline hook config destination: %w", err)
	}

	configPath, err := resolver(configDestination)
	if err != nil {
		return err
	}
	var decoded map[string]toml.Primitive
	metadata, err := toml.DecodeFile(configPath, &decoded)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("observe destination %q: parse %q for Codex inline hooks: %w", destination, configDestination, err)
	}
	if metadata.IsDefined("hooks") {
		return fmt.Errorf("observe destination %q: unmanaged Codex inline hooks found in %q", destination, configDestination)
	}

	return nil
}
