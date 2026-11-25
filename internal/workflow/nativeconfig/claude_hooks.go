package nativeconfig

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/isty2e/daem/internal/assurance/durable"
	physicalhostpath "github.com/isty2e/daem/internal/assurance/observe/hostpath"
	"github.com/isty2e/daem/internal/desired/entity"
	"github.com/isty2e/daem/internal/desired/hook"
	"github.com/isty2e/daem/internal/effect/mutation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/findings"
	"github.com/isty2e/daem/internal/output/ownership"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/target"
	targetselection "github.com/isty2e/daem/internal/target/selection"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
	"github.com/isty2e/daem/internal/workflow/readiness"
)

// SelectClaudeHookSettings chooses one lookup for the complete hook set.
// A pre-profile baseline always denotes the literal home placement.
func SelectClaudeHookSettings(paths daempaths.Paths, before durable.Snapshot, hooks []hook.Hook, selection targetselection.Selection, budget rootedpath.PhysicalTraversalBudget) (durable.ClaudeHookSettings, []findings.Diagnostic, error) {
	if !selection.Includes(target.TargetClaudeCode) {
		return durable.ClaudeHookSettings{}, nil, nil
	}
	var id entity.ID
	member, established := before.ClaudeGlobalHookMember()
	if established {
		id, _ = topologyprojection.EntityID(member.Subject()) // Managed hook subjects are entity-backed.
	} else {
		for _, declaration := range hooks {
			if declaration.Scope() == target.ScopeGlobal && slices.Contains(declaration.Targets(), target.TargetClaudeCode) {
				id = declaration.ID()
				break
			}
		}
	}
	if id == (entity.ID{}) {
		return durable.ClaudeHookSettings{}, nil, nil
	}

	var settings durable.ClaudeHookSettings
	var err error
	if established {
		settings, err = InstalledClaudeHookSettings(paths, before)
	} else {
		settings, err = selectClaudeHookFile(paths, budget)
	}
	if err != nil {
		return durable.ClaudeHookSettings{}, nil, err
	}
	return settings, []findings.Diagnostic{{
		Severity: findings.SeverityOK, Code: "claude_global_hook_settings", EntityID: id,
		Target: target.TargetClaudeCode, Scope: target.ScopeGlobal, Detail: fmt.Sprintf("Claude global hook settings: %s (destination %s)", settings.Path(), aggregate.ClaudeGlobalHookConfigPath),
	}}, nil
}

// ClaudeHookProfileMismatch identifies two different files before the native
// ownership check can mistake the absent claim in the second file for corruption.
type ClaudeHookProfileMismatch struct {
	hookSettings, nativeSettings string
}

func (m *ClaudeHookProfileMismatch) Error() string {
	return fmt.Sprintf("Claude global hooks are managed in %q, but the selected global agent_config uses %q", m.hookSettings, m.nativeSettings)
}

func (m *ClaudeHookProfileMismatch) RetryInstruction() string {
	return fmt.Sprintf("set CLAUDE_CONFIG_DIR to %q and rerun the same command to use the profile that owns the hooks", filepath.Dir(m.hookSettings))
}

func checkInstalledClaudeHookProfile(settings string, before durable.Snapshot, selected []aggregate.DocumentAddress, native *readiness.NativeAssessmentInput) error {
	if settings == "" {
		return nil
	}
	member, installed := before.ClaudeGlobalHookMember()
	if !installed {
		return nil
	}
	hook := member.Contribution()
	document := hook.Address().Document()
	if !slices.Contains(selected, document) {
		return nil
	}
	for _, namespace := range native.Documents.Evidence().Namespaces() {
		if !slices.Contains(namespace.Addresses(), document) {
			continue
		}
		_, observed, err := mutation.ObserveFutureReferentAuthorityBounded(settings, physicalwork.MaximumPhysicalPathDepth, native.Budget)
		if err != nil || observed.Equal(namespace.Observation()) {
			return err
		}

		// Only recommend a profile when its live claim agrees with the saved location.
		exact, present := observed.Exact()
		if !present {
			return ownership.ErrClaimMissing
		}
		address, err := ownership.NewManagedAddress(exact, hook.Footprint())
		if err != nil {
			return err
		}
		claim, present := native.Registry.Exact(address)
		if !present || !claim.OwnedBy(native.Owner) || claim.State() != ownership.ClaimActive {
			return ownership.ErrClaimMissing
		}
		keySettings, err := namespace.WriterPath()
		if err != nil {
			return err
		}
		return &ClaudeHookProfileMismatch{hookSettings: settings, nativeSettings: keySettings}
	}
	return nil
}

// InstalledClaudeHookSettings never selects a new profile. Historical state
// without a saved location used literal home, without Claude's NFC normalization.
func InstalledClaudeHookSettings(paths daempaths.Paths, before durable.Snapshot) (durable.ClaudeHookSettings, error) {
	if saved := before.ClaudeHookSettings(); saved.Path() != "" {
		return saved, nil
	}
	if _, installed := before.ClaudeGlobalHookMember(); !installed {
		return durable.ClaudeHookSettings{}, nil
	}
	home, err := paths.UserHomeDir()
	if err != nil {
		return durable.ClaudeHookSettings{}, err
	}
	return durable.NewClaudeHookSettings(filepath.Join(home, ".claude", "settings.json"))
}

func selectClaudeHookFile(paths daempaths.Paths, budget rootedpath.PhysicalTraversalBudget) (durable.ClaudeHookSettings, error) {
	path, err := physicalhostpath.NewResolver(paths).ClaudeUserSettings(budget)
	if err != nil {
		return durable.ClaudeHookSettings{}, fmt.Errorf("resolve Claude global hook directory: %w", err)
	}
	return durable.NewClaudeHookSettings(path)
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
	selected, err := selectClaudeHookFile(paths, budget)
	if err != nil {
		return err
	}
	if selected != recorded {
		return fmt.Errorf("interrupted Claude hook operation used %q; run recover with CLAUDE_CONFIG_DIR set to %q", recorded.Path(), filepath.Dir(recorded.Path()))
	}
	return nil
}
