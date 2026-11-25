package durable

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/topology"
)

// ClaudeHookSettings is the installed hook set's lookup location, not write authority.
type ClaudeHookSettings struct{ path string }

func NewClaudeHookSettings(path string) (ClaudeHookSettings, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "settings.json" || strings.ContainsRune(path, '\x00') || !utf8.ValidString(path) {
		return ClaudeHookSettings{}, fmt.Errorf("Claude hook settings must name an absolute, clean settings.json path")
	}
	return ClaudeHookSettings{path: path}, nil
}

func (settings ClaudeHookSettings) Path() string { return settings.path }

func claudeGlobalHookMember(rows []ManagedAggregateState) (topology.SubjectID, bool) {
	for _, row := range rows {
		if row.contribution.CodecContractID() == aggregate.HookCodecClaudeGlobal {
			return row.subject, true
		}
	}
	return topology.SubjectID{}, false
}

func (snapshot Snapshot) ClaudeGlobalHookMember() (topology.SubjectID, bool) {
	return claudeGlobalHookMember(snapshot.managedAggregates)
}

func (snapshot Snapshot) ClaudeHookSettings() ClaudeHookSettings { return snapshot.claudeHookSettings }

// ClaudeHookPlacementForTransition rejects retargeting a surviving hook set,
// including an attempt to reinterpret a historical literal-home installation.
func (snapshot Snapshot) ClaudeHookPlacementForTransition(after Snapshot) (ClaudeHookSettings, error) {
	if _, existed := snapshot.ClaudeGlobalHookMember(); existed {
		if _, retained := after.ClaudeGlobalHookMember(); retained && snapshot.claudeHookSettings != after.claudeHookSettings {
			return ClaudeHookSettings{}, fmt.Errorf("a surviving Claude global hook set cannot change settings files")
		}
		return snapshot.claudeHookSettings, nil
	}
	return after.claudeHookSettings, nil
}

// WithClaudeHookPlacement records a first installation, preserves the original
// lookup for surviving members, and forgets it when the last hook leaves.
func (snapshot Snapshot) WithClaudeHookPlacement(before Snapshot, selected string) (Snapshot, error) {
	if _, installed := snapshot.ClaudeGlobalHookMember(); !installed {
		return snapshot, nil
	}
	if _, retained := before.ClaudeGlobalHookMember(); retained {
		if old := before.claudeHookSettings.Path(); old != "" && selected != "" && old != selected {
			return Snapshot{}, fmt.Errorf("ordinary apply cannot move established Claude hooks")
		}
		snapshot.claudeHookSettings = before.claudeHookSettings
	} else if selected != "" {
		location, err := NewClaudeHookSettings(selected)
		if err != nil {
			return Snapshot{}, err
		}
		snapshot.claudeHookSettings = location
	}
	return snapshot, nil
}
