package record

import (
	"fmt"

	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/realization/profile/configuration"
	"github.com/isty2e/daem/internal/target"
)

func native16ManifestRootReference() (output.RootReference, error) {
	return output.NewRootReference("project", "selected-project-v1")
}

// Native16 did not record bindings. Keep its admitted interpretation independent
// of the current catalog; third-host native namespaces were not admitted by it.
func native16RootBindingForAlias(address aggregate.DocumentAddress) (configuration.AliasLocation, error) {
	selected := address.Target()
	if selected != target.TargetPi && selected != target.TargetCodex {
		return configuration.AliasLocation{}, fmt.Errorf("native16 document alias has an unsupported host")
	}
	destination := address.AggregateRoot()
	if err := destination.ValidateScope(address.Scope()); err != nil {
		return configuration.AliasLocation{}, err
	}
	relative := destination.RelativePath()
	var reference output.RootReference
	var err error
	switch address.Scope() {
	case target.ScopeProject:
		if destination.RootRole() != output.RootProject {
			return configuration.AliasLocation{}, fmt.Errorf("native16 project alias requires its manifest root")
		}
		reference, err = native16ManifestRootReference()
	case target.ScopeGlobal:
		switch selected {
		case target.TargetPi:
			if destination.String() == "~/.pi/agent/mcp.json" {
				relative = "mcp.json"
			} else if destination.RootRole() != output.RootPiAgent {
				return configuration.AliasLocation{}, fmt.Errorf("%w: native16 Pi alias has no recorded-version root binding", configuration.ErrDocumentPath)
			}
			reference, err = output.NewRootReference("pi-agent", "pi-agent-location-v1")
		case target.TargetCodex:
			if destination.String() == "~/.codex/config.toml" {
				relative = "config.toml"
			} else if destination.RootRole() != output.RootCodexHome {
				return configuration.AliasLocation{}, fmt.Errorf("%w: native16 Codex alias has no recorded-version root binding", configuration.ErrDocumentPath)
			}
			reference, err = output.NewRootReference("codex-home", "codex-home-location-v1")
		}
	default:
		return configuration.AliasLocation{}, fmt.Errorf("native16 alias requires an admitted scope")
	}
	if err != nil {
		return configuration.AliasLocation{}, err
	}
	return configuration.NewAliasLocation(reference, relative)
}
