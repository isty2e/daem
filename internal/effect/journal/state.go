package journal

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/isty2e/daem/internal/assurance/durable"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/realization"
	"github.com/isty2e/daem/internal/realization/profile"
	"github.com/isty2e/daem/internal/supply/artifact"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
	topologyprojection "github.com/isty2e/daem/internal/topology/projection"
)

type recoveryManagedState struct {
	identity    recoveryStateIdentity
	contentHash string
}

type recoveryStateKey struct {
	subject     topology.SubjectID
	target      string
	scope       string
	path        string
	contentPath string
}

func recoveryStateKeyForAction(action pathMutation) recoveryStateKey {
	return recoveryStateKey{
		subject:     action.Subject,
		target:      string(action.Target),
		scope:       string(action.Scope),
		path:        action.Destination.String(),
		contentPath: string(action.ContentPath),
	}
}

func recoveryStateKeyForPreviousState(previous previousPathState) recoveryStateKey {
	return recoveryStateKey{
		subject:     previous.Subject,
		target:      string(previous.Target),
		scope:       string(previous.Scope),
		path:        previous.Destination.String(),
		contentPath: string(previous.ContentPath),
	}
}

func recoveryStateKeyForManagedPath(state durable.ManagedPathState) recoveryStateKey {
	return recoveryStateKey{
		subject: state.Subject(),
		scope:   string(state.Scope()),
		path:    state.Destination().String(),
	}
}

func captureRecoveryStateBefore(action pathMutation, state map[recoveryStateKey]recoveryManagedState) (recoveryManagedMembership, error) {
	if action.StateIndependent {
		return recoveryManagedMembership{Managed: false}, nil
	}
	if action.PreviousState == nil {
		if _, exists := state[recoveryStateKeyForAction(action)]; exists {
			return recoveryManagedMembership{}, fmt.Errorf("state observation for %q does not match action without state", action.Destination)
		}
		return recoveryManagedMembership{Managed: false}, nil
	}

	current, ok := state[recoveryStateKeyForPreviousState(*action.PreviousState)]
	if !ok {
		return recoveryManagedMembership{}, fmt.Errorf("missing state observation for %q", action.Destination)
	}
	if current.contentHash != string(action.PreviousState.ContentHash) {
		return recoveryManagedMembership{}, fmt.Errorf("state observation %q hash %q does not match action previous state hash %q", action.Destination, current.contentHash, action.PreviousState.ContentHash)
	}
	if err := validateRecoveryManagedIdentity(current, recoveryStateIdentityFromPrevious(*action.PreviousState)); err != nil {
		return recoveryManagedMembership{}, fmt.Errorf("state observation %q identity does not match action previous state: %w", action.Destination, err)
	}

	return recoveryManagedMembership{
		Managed:     true,
		ContentHash: current.contentHash,
	}, nil
}

func recoveryStateByAction(snapshot durable.Snapshot) (map[recoveryStateKey]recoveryManagedState, error) {
	paths := snapshot.ManagedPaths()
	state := make(map[recoveryStateKey]recoveryManagedState, len(paths))
	for _, managedPath := range paths {
		key := recoveryStateKeyForManagedPath(managedPath)
		if _, exists := state[key]; exists {
			return nil, fmt.Errorf("duplicate managed state for %q", managedPath.Destination())
		}
		state[key] = recoveryManagedState{
			identity:    recoveryStateIdentityFromManagedPath(managedPath),
			contentHash: string(managedPath.ContentHash()),
		}
	}

	return state, nil
}

func validateRecoveryJournalStatefile(snapshot durable.Snapshot, entries []recoveryEntry, before bool) error {
	paths := snapshot.ManagedPaths()
	stateByKey := make(map[recoveryStateKey]recoveryManagedState, len(paths))
	for _, state := range paths {
		key := recoveryStateKeyForManagedPath(state)
		stateByKey[key] = recoveryManagedState{
			identity:    recoveryStateIdentityFromManagedPath(state),
			contentHash: string(state.ContentHash()),
		}
	}

	stateName := "statefile_after"
	if before {
		stateName = "statefile_before"
	}
	for index, entry := range entries {
		if entry.StateIndependent {
			continue
		}
		expected := entry.StateExpectedAfter
		expectedIdentity := recoveryStateIdentityFromEntry(entry)
		key, err := recoveryStateKeyForEntry(entry)
		if err != nil {
			return fmt.Errorf("recovery journal %s entries[%d]: %w", stateName, index, err)
		}
		if before {
			expected = entry.StateBefore
			if entry.StateBeforeIdentity != nil {
				expectedIdentity = *entry.StateBeforeIdentity
				key, err = recoveryStateKeyForPersistedIdentity(*entry.StateBeforeIdentity)
				if err != nil {
					return fmt.Errorf("recovery journal %s entries[%d].state_before_identity: %w", stateName, index, err)
				}
			}
		}
		if err := validateRecoveryEntryState(stateByKey, key, expectedIdentity, expected); err != nil {
			return fmt.Errorf("recovery journal %s does not match entries[%d]: %w", stateName, index, err)
		}
	}

	return nil
}

func recoveryStateKeyForPersistedIdentity(identity recoveryStateIdentity) (recoveryStateKey, error) {
	subject, err := identity.Subject.canonical()
	if err != nil {
		return recoveryStateKey{}, err
	}
	return recoveryStateKey{
		subject:     subject,
		target:      identity.Target,
		scope:       identity.Scope,
		path:        identity.Path,
		contentPath: identity.ContentPath,
	}, nil
}

func validateRecoveryEntryState(
	state map[recoveryStateKey]recoveryManagedState,
	key recoveryStateKey,
	identity recoveryStateIdentity,
	expected recoveryManagedMembership,
) error {
	resource, exists := state[key]
	if !expected.Managed {
		if exists {
			return fmt.Errorf("expected unmanaged state for %q, found managed hash %q", key.path, resource.contentHash)
		}

		return nil
	}
	if !exists {
		return fmt.Errorf("expected managed state for %q", key.path)
	}
	if resource.contentHash != expected.ContentHash {
		return fmt.Errorf("expected state hash %q for %q, found %q", expected.ContentHash, key.path, resource.contentHash)
	}
	if err := validateRecoveryManagedIdentity(resource, identity); err != nil {
		return err
	}

	return nil
}

func recoveryStateIdentityFromEntry(entry recoveryEntry) recoveryStateIdentity {
	return recoveryStateIdentity{
		Subject: entry.Subject, Target: entry.Target,
		Targets: append([]string(nil), entry.Targets...), Scope: entry.Scope, Path: entry.Path,
		ContentPath: entry.ContentPath, ContentKind: entry.ContentKind, Aggregate: entry.Aggregate != nil,
	}
}

type recoveryEntryKey struct {
	subject     topology.SubjectID
	target      string
	targets     string
	scope       string
	path        string
	contentPath string
}

func validateRecoveryEntries(entries []recoveryEntry) error {
	seen := make(map[recoveryEntryKey]struct{}, len(entries))
	for index, entry := range entries {
		context := fmt.Sprintf("recovery entries[%d]", index)
		if entry.ContentPath != "" && entry.Aggregate == nil {
			return fmt.Errorf("%s.aggregate: content path requires a projection contract", context)
		}
		identity := recoveryStateIdentityFromEntry(entry)
		if err := validateRecoveryStateIdentity(identity); err != nil {
			return fmt.Errorf("%s state identity: %w", context, err)
		}
		switch entry.Scope {
		case string(target.ScopeGlobal):
			if err := validateRecoveryResolvedGlobalPath(entry.ResolvedGlobalPath); err != nil {
				return fmt.Errorf("%s.resolved_global_path: %w", context, err)
			}
		case string(target.ScopeProject):
			if entry.ResolvedGlobalPath != "" {
				return fmt.Errorf("%s.resolved_global_path: project entry must not carry a global path binding", context)
			}
		}
		if err := recovery.ValidateBefore(entry.Before.canonical(), entry.ContentPath); err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
		if err := validateRecoveryContentHash(entry.Before.ContentHash, "before.content_hash"); err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
		if err := recovery.ValidateExpected(entry.ExpectedAfter.canonical(), entry.ContentPath); err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
		if err := validateRecoveryContentHash(entry.ExpectedAfter.ContentHash, "expected_after.content_hash"); err != nil {
			return fmt.Errorf("%s: %w", context, err)
		}
		if err := validateRecoveryManagedMembership(entry.StateBefore); err != nil {
			return fmt.Errorf("%s.state_before: %w", context, err)
		}

		subject, err := entry.Subject.canonical()
		if err != nil {
			return fmt.Errorf("%s state identity: %w", context, err)
		}
		key := recoveryEntryKey{
			subject: subject, target: entry.Target,
			targets: strings.Join(entry.Targets, "\x00"), scope: entry.Scope,
			path: entry.Path, contentPath: entry.ContentPath,
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("%s: duplicate recovery entry for %q content_path %q", context, entry.Path, entry.ContentPath)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateRecoveryResolvedGlobalPath(value string) error {
	if value == "" {
		return fmt.Errorf("global entry requires its capture-time resolved path")
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("resolved path contains a NUL byte")
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("resolved path must be valid UTF-8")
	}
	if !filepath.IsAbs(value) {
		return fmt.Errorf("resolved path %q must be absolute", value)
	}
	if filepath.Clean(value) != value {
		return fmt.Errorf("resolved path %q must be clean", value)
	}
	return nil
}

func validateRecoveryContentHash(value string, context string) error {
	if value == "" {
		return nil
	}
	if err := artifact.ContentHash(value).Validate(); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	return nil
}

func validateRecoveryManagedMembership(state recoveryManagedMembership) error {
	if state.Managed {
		if state.ContentHash == "" {
			return fmt.Errorf("managed state content hash is required")
		}
		if err := artifact.ContentHash(state.ContentHash).Validate(); err != nil {
			return fmt.Errorf("managed state content hash: %w", err)
		}
		return nil
	}
	if state.ContentHash != "" {
		return fmt.Errorf("unmanaged state must not contain a content hash")
	}
	return nil
}

func validateRecoveryStateIdentity(identity recoveryStateIdentity) error {
	subject, err := identity.Subject.canonical()
	if err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	managedPath := identity.ContentKind != ""
	if identity.Aggregate {
		if _, err := target.ParseTarget(identity.Target); err != nil {
			return err
		}
		if len(identity.Targets) != 0 || identity.ContentKind != "" {
			return fmt.Errorf("managed aggregate identity requires one primary target and no path content kind")
		}
	} else if managedPath {
		if _, entityBacked := topologyprojection.EntityID(subject); !entityBacked {
			return fmt.Errorf("managed path identity requires entity-backed projection subject")
		}
		if identity.Target != "" || len(identity.Targets) == 0 {
			return fmt.Errorf("managed path identity requires consumer targets and no primary target")
		}
	} else {
		if _, entityBacked := topologyprojection.EntityID(subject); entityBacked {
			return fmt.Errorf("entity-backed projection identity requires an explicit realization discriminator")
		}
		if _, err := target.ParseTarget(identity.Target); err != nil {
			return err
		}
	}
	if len(identity.Targets) != 0 {
		previous := ""
		seenTargets := make(map[string]struct{}, len(identity.Targets))
		containsPrimary := false
		for index, value := range identity.Targets {
			if _, err := target.ParseTarget(value); err != nil {
				return fmt.Errorf("targets[%d]: %w", index, err)
			}
			if _, duplicate := seenTargets[value]; duplicate {
				return fmt.Errorf("targets must be duplicate-free")
			}
			if managedPath && index > 0 && value <= previous {
				return fmt.Errorf("targets must be sorted and duplicate-free")
			}
			seenTargets[value] = struct{}{}
			containsPrimary = containsPrimary || value == identity.Target
			previous = value
		}
		if !managedPath && !identity.Aggregate && !containsPrimary {
			return fmt.Errorf("consumer targets must include primary target %q", identity.Target)
		}
	}
	scope, err := target.ParseScope(identity.Scope)
	if err != nil {
		return err
	}
	destination, err := output.Parse(identity.Path)
	if err != nil {
		return fmt.Errorf("path: %w", err)
	}
	if err := destination.ValidateScope(scope); err != nil {
		return fmt.Errorf("path: %w", err)
	}
	if identity.ContentPath != "" && (!strings.HasPrefix(identity.ContentPath, "/") || strings.TrimSpace(identity.ContentPath) != identity.ContentPath) {
		return fmt.Errorf("content path must be absolute and canonical")
	}
	if managedPath {
		if identity.ContentPath != "" {
			return fmt.Errorf("managed path identity must own the whole path")
		}
		if _, err := managedPathStateKind(realization.PathProjectionContentKind(identity.ContentKind)); err != nil {
			return err
		}
		entityID, _ := topologyprojection.EntityID(subject)
		consumers := make([]target.Target, 0, len(identity.Targets))
		for _, value := range identity.Targets {
			consumers = append(consumers, target.Target(value))
		}
		if err := profile.ValidateManagedPathOccupancy(
			entityID,
			subject.Namespace(),
			consumers,
			target.Scope(identity.Scope),
			identity.Path,
			realization.PathProjectionContentKind(identity.ContentKind),
		); err != nil {
			return fmt.Errorf("managed path occupancy: %w", err)
		}
	} else if identity.ContentKind != "" {
		return fmt.Errorf("non-path identity must not carry content kind %q", identity.ContentKind)
	}
	return nil
}

func validateRecoveryManagedIdentity(actual recoveryManagedState, expected recoveryStateIdentity) error {
	actualIdentity := actual.identity
	actualSubject, err := actualIdentity.Subject.canonical()
	if err != nil {
		return err
	}
	expectedSubject, err := expected.Subject.canonical()
	if err != nil {
		return err
	}
	if actualSubject != expectedSubject ||
		actualIdentity.Target != expected.Target || actualIdentity.Scope != expected.Scope ||
		actualIdentity.Path != expected.Path || actualIdentity.ContentPath != expected.ContentPath ||
		actualIdentity.ContentKind != expected.ContentKind || actualIdentity.Aggregate != expected.Aggregate {
		return fmt.Errorf("state identity for %q does not match journal identity", expected.Path)
	}
	expectedTargets := append([]string(nil), expected.Targets...)
	if len(expectedTargets) == 0 && expected.Target != "" {
		expectedTargets = []string{expected.Target}
	}
	if !slices.Equal(actualIdentity.Targets, expectedTargets) {
		return fmt.Errorf("state consumers %v for %q do not match journal consumers %v", actualIdentity.Targets, expected.Path, expectedTargets)
	}
	return nil
}

func recoveryStateIdentityFromManagedPath(state durable.ManagedPathState) recoveryStateIdentity {
	return recoveryStateIdentity{
		Subject: persistedSubjectRef{
			Kind:      string(state.Subject().Kind()),
			Namespace: state.Subject().Namespace(),
			Name:      state.Subject().Key(),
		},
		Targets:     targetStrings(state.ConsumerTargets()),
		Scope:       string(state.Scope()),
		Path:        state.Destination().String(),
		ContentKind: string(state.ContentKind()),
	}
}

func recoveryStateKeyForEntry(entry recoveryEntry) (recoveryStateKey, error) {
	subject, err := entry.Subject.canonical()
	if err != nil {
		return recoveryStateKey{}, err
	}
	return recoveryStateKey{
		subject:     subject,
		target:      entry.Target,
		scope:       entry.Scope,
		path:        entry.Path,
		contentPath: entry.ContentPath,
	}, nil
}

func sortRecoveryEntries(entries []recoveryEntry) {
	sort.SliceStable(entries, func(leftIndex int, rightIndex int) bool {
		left := entries[leftIndex]
		right := entries[rightIndex]
		if left.Target != right.Target {
			return left.Target < right.Target
		}
		if strings.Join(left.Targets, "\x00") != strings.Join(right.Targets, "\x00") {
			return strings.Join(left.Targets, "\x00") < strings.Join(right.Targets, "\x00")
		}
		if left.Scope != right.Scope {
			return left.Scope < right.Scope
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.ContentPath != right.ContentPath {
			return left.ContentPath < right.ContentPath
		}
		if left.Subject.Kind != right.Subject.Kind {
			return left.Subject.Kind < right.Subject.Kind
		}
		if left.Subject.Namespace != right.Subject.Namespace {
			return left.Subject.Namespace < right.Subject.Namespace
		}
		if left.Subject.Name != right.Subject.Name {
			return left.Subject.Name < right.Subject.Name
		}
		return false
	})
}
