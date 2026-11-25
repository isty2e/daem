package nativeconfig

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/findings"
	nativehostpath "github.com/isty2e/daem/internal/output/hostpath/native"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

// SelectClaudeHookSettings chooses one lookup for the complete hook set.
// A pre-profile baseline always denotes the literal home placement.
func SelectClaudeHookSettings(paths daempaths.Paths, before durable.Snapshot, hooks []hook.Hook, selection targetselection.Selection, budget rootedpath.PhysicalTraversalBudget) (string, []findings.Diagnostic, error) {
	if !selection.Includes(target.TargetClaudeCode) {
		return "", nil, nil
	}
	var id entity.ID
	member, established := before.ClaudeGlobalHookMember()
	if established {
		id, _ = topologyprojection.EntityID(member) // Managed hook subjects are entity-backed.
	} else {
		for _, declaration := range hooks {
			if declaration.Scope() == target.ScopeGlobal && slices.Contains(declaration.Targets(), target.TargetClaudeCode) {
				id = declaration.ID()
				break
			}
		}
	}
	if id == (entity.ID{}) {
		return "", nil, nil
	}

	settings := before.ClaudeHookSettings()
	if settings.Path() == "" {
		var err error
		settings, err = selectClaudeHookFile(paths, established, budget)
		if err != nil {
			return "", nil, err
		}
	}
	return settings.Path(), []findings.Diagnostic{{
		Severity: findings.SeverityOK, Code: "claude_global_hook_settings", EntityID: id,
		Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Detail: "Claude global hook settings: " + settings.Path(),
	}}, nil
}

func selectClaudeHookFile(paths daempaths.Paths, legacy bool, budget rootedpath.PhysicalTraversalBudget) (durable.ClaudeHookSettings, error) {
	home, err := paths.UserHomeDir()
	if err != nil {
		return durable.ClaudeHookSettings{}, err
	}
	if legacy {
		// Historical hooks used literal home expansion, without Claude's NFC normalization.
		return durable.NewClaudeHookSettings(filepath.Join(home, ".claude", "settings.json"))
	}
	value, present := os.LookupEnv("CLAUDE_CONFIG_DIR")
	dir, err := nativehostpath.ClaudeConfigurationDirectory(value, present, paths.ManifestRoot, home)
	if err != nil {
		return durable.ClaudeHookSettings{}, err
	}
	dir, err = rootedpath.ResolveFutureReferentPathBounded(dir, physicalwork.MaximumPhysicalPathDepth, budget)
	if err != nil {
		return durable.ClaudeHookSettings{}, fmt.Errorf("resolve Claude global hook directory: %w", err)
	}
	return durable.NewClaudeHookSettings(filepath.Join(dir, "settings.json"))
}

// Recovery needs a selector independent of the journal. Before the first
// state commit (or after final removal), the original profile supplies it.
func QualifyClaudeHookRecovery(ctx context.Context, paths daempaths.Paths, recorded durable.ClaudeHookSettings, read durable.SnapshotReader, budget rootedpath.PhysicalTraversalBudget) error {
	current, err := read(ctx)
	if err != nil {
		return err
	}
	if _, installed := current.ClaudeGlobalHookMember(); installed {
		if current.ClaudeHookSettings() == recorded {
			return nil
		}
		return fmt.Errorf("recorded Claude hook settings differ from the installed hook set")
	}
	selected, err := selectClaudeHookFile(paths, false, budget)
	if err != nil {
		return err
	}
	if selected != recorded {
		return fmt.Errorf("interrupted Claude hook operation used %q; run recover with CLAUDE_CONFIG_DIR set to %q", recorded.Path(), filepath.Dir(recorded.Path()))
	}
	return nil
}
