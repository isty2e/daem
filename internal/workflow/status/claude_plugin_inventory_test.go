package status

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/assurance/durable"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	relationhost "github.com/isty2e/daem/internal/assurance/observe/relation/host"
	"github.com/isty2e/daem/internal/assurance/stateauthority"
	realizationdelegate "github.com/isty2e/daem/internal/realization/delegate"
	lock "github.com/isty2e/daem/internal/realization/lock"
)

func TestRelationObserverCorrelationIsIndependentOfPriorCarrierState(t *testing.T) {
	root := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	writeTestFile(t, root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	writeTestFile(t, configRoot, "plugins/installed_plugins.json", `{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":"`+root+`"}]}}`)
	locked, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	record := locked.Locked.Subjects()[0]
	pending := relationPendingForLockedRecord(t, root, record)
	claim := relationClaimForPending(t, pending)
	stale := relationPendingWithHash(
		t,
		pending,
		"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
	)

	tests := []struct {
		name        string
		state       durable.Snapshot
		onlySubject bool
		want        observerelation.CorrelationState
	}{
		{name: "no prior state", state: durable.EmptySnapshot(), want: observerelation.StateExactCorrelation},
		{name: "observation filter grants no claim", state: durable.EmptySnapshot(), onlySubject: true, want: observerelation.StateExactCorrelation},
		{name: "exact managed claim", state: relationCarrierFactSnapshot(t, nil, []durablecarrier.ManagedCarrierClaim{claim}), want: observerelation.StateExactCorrelation},
		{name: "exact pending install", state: relationCarrierFactSnapshot(t, []durablecarrier.PendingCarrierInstall{pending}, nil), want: observerelation.StateExactCorrelation},
		{
			name:  "nonmatching pending install",
			state: relationCarrierFactSnapshot(t, []durablecarrier.PendingCarrierInstall{stale}, nil),
			want:  observerelation.StateExactCorrelation,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := relationhost.Input{
				Paths:                resolveTestPaths(t, manifestPath),
				Lockfile:             locked,
				ManagedCarrierClaims: test.state.ManagedCarrierClaims(),
				Selection:            statusClaudeSelection(t, "claude-code"),
			}
			key := relationCorrelationKeyForLockedRecord(t, record)
			if test.onlySubject {
				input.OnlyCorrelation = &key
			}
			observations, err := relationhost.Observe(context.Background(), input)
			if err != nil {
				t.Fatalf("relationhost.Observe returned error: %v", err)
			}
			correlation, ok := observations.Correlation(key)
			if !ok {
				t.Fatal("relationhost.Observe returned no correlation")
			}
			if got := correlation.State(); got != test.want {
				t.Fatalf("correlation state = %q, want %q", got, test.want)
			}
		})
	}
}

func TestObservationOnlySubjectIsolatedToExactCarrierSubject(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	writeTestFile(t, configRoot, "plugins/installed_plugins.json", `{malformed`)
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	if err := os.Mkdir(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	claudeLocked, _ := statusClaudePluginExtensionLockfile(t, "claude", "plugin@market")
	codexLocked, _ := statusCodexPluginExtensionLockfile(t, "codex", "plugin@market")
	mixedLocked := statusLockfileFromRecords(
		t,
		claudeLocked.Locked.Subjects()[0],
		codexLocked.Locked.Subjects()[0],
	)
	selection := statusClaudeSelection(t, "claude-code", "codex")
	codexRecord := codexLocked.Locked.Subjects()[0]
	claudeRecord := claudeLocked.Locked.Subjects()[0]
	codexKey := relationCorrelationKeyForLockedRecord(t, codexRecord)
	claudeKey := relationCorrelationKeyForLockedRecord(t, claudeRecord)

	observations, err := relationhost.Observe(context.Background(), relationhost.Input{
		Lockfile:        mixedLocked,
		Selection:       selection,
		OnlyCorrelation: &codexKey,
	})
	if err != nil {
		t.Fatalf("Codex observation-only selection read unrelated Claude inventory: %v", err)
	}
	codexCorrelation, ok := observations.Correlation(codexKey)
	if !ok || codexCorrelation.State() != observerelation.StateMissing {
		t.Fatalf("Codex observation-only correlation = %#v/%t, want missing", codexCorrelation, ok)
	}
	canonicalCodexHome, err := filepath.EvalSymlinks(codexHome)
	if err != nil {
		t.Fatal(err)
	}
	authorityPaths := observations.AuthorityPaths()
	if len(authorityPaths) != 1 ||
		authorityPaths[0].Path() != filepath.Join(canonicalCodexHome, "config.toml") {
		t.Fatalf("Codex observation-only authority paths = %#v", authorityPaths)
	}

	if _, err := relationhost.Observe(context.Background(), relationhost.Input{
		Lockfile:        mixedLocked,
		Selection:       selection,
		OnlyCorrelation: &claudeKey,
	}); err == nil {
		t.Fatal("Claude observation-only selection hid its malformed inventory")
	}
}

func relationCorrelationKeyForLockedRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
) observerelation.CorrelationKey {
	t.Helper()
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil {
		t.Fatalf("ManagedCarrierIdentityFromLockedRecord returned error: %v", err)
	}
	if !admitted {
		t.Fatal("locked record is not an admitted carrier")
	}
	key, err := observerelation.NewCorrelationKey(
		identity.RelationSubject(),
		identity.ExpectedRelation(),
	)
	if err != nil {
		t.Fatalf("NewCorrelationKey returned error: %v", err)
	}
	return key
}

func TestRelationObserverIncludesRetainedClaimAbsentFromCurrentLock(t *testing.T) {
	root := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	writeTestFile(t, root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	writeTestFile(t, configRoot, "plugins/installed_plugins.json", `{"version":2,"plugins":{"context7@market":[{"scope":"project","projectPath":"`+root+`"}]}}`)

	previousLock, _ := statusClaudePluginExtensionLockfile(t, "context7", "context7@market")
	previousRecord := previousLock.Locked.Subjects()[0]
	pending := relationPendingForLockedRecord(t, root, previousRecord)
	claim := relationClaimForPending(t, pending)
	key := relationCorrelationKeyForLockedRecord(t, previousRecord)

	observations, err := relationhost.Observe(context.Background(), relationhost.Input{
		Paths:                resolveTestPaths(t, manifestPath),
		Lockfile:             lock.File{Version: lock.CurrentVersion},
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
		Selection:            statusClaudeSelection(t, "claude-code"),
	})
	if err != nil {
		t.Fatalf("relationhost.Observe returned error: %v", err)
	}
	correlation, ok := observations.Correlation(key)
	if !ok {
		t.Fatal("retained managed claim has no passive correlation")
	}
	if got := correlation.State(); got != observerelation.StateExactCorrelation {
		t.Fatalf("retained claim correlation state = %q, want %q", got, observerelation.StateExactCorrelation)
	}
}

func TestRelationObserverKeepsOldAndNewReplacementExpectationsDistinct(t *testing.T) {
	root := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	manifestPath := filepath.Join(root, "daem.toml")
	writeTestFile(t, root, "daem.toml", "version = 1\ntargets = [\"claude-code\"]\n")
	writeTestFile(t, configRoot, "plugins/installed_plugins.json", `{"version":2,"plugins":{"old@market":[{"scope":"project","projectPath":"`+root+`"}]}}`)

	previousLock, _ := statusClaudePluginExtensionLockfile(t, "shared-id", "old@market")
	currentLock, _ := statusClaudePluginExtensionLockfile(t, "shared-id", "new@market")
	previousRecord := previousLock.Locked.Subjects()[0]
	currentRecord := currentLock.Locked.Subjects()[0]
	if previousRecord.SubjectID() != currentRecord.SubjectID() {
		t.Fatal("replacement fixture did not reuse the same relation subject")
	}
	pending := relationPendingForLockedRecord(t, root, previousRecord)
	claim := relationClaimForPending(t, pending)
	previousKey := relationCorrelationKeyForLockedRecord(t, previousRecord)
	currentKey := relationCorrelationKeyForLockedRecord(t, currentRecord)

	observations, err := relationhost.Observe(context.Background(), relationhost.Input{
		Paths:                resolveTestPaths(t, manifestPath),
		Lockfile:             currentLock,
		ManagedCarrierClaims: []durablecarrier.ManagedCarrierClaim{claim},
		Selection:            statusClaudeSelection(t, "claude-code"),
	})
	if err != nil {
		t.Fatalf("relationhost.Observe returned error: %v", err)
	}
	previousCorrelation, ok := observations.Correlation(previousKey)
	if !ok {
		t.Fatal("previous replacement expectation has no correlation")
	}
	if got := previousCorrelation.State(); got != observerelation.StateExactCorrelation {
		t.Fatalf("previous replacement state = %q, want %q", got, observerelation.StateExactCorrelation)
	}
	currentCorrelation, ok := observations.Correlation(currentKey)
	if !ok {
		t.Fatal("current replacement expectation has no correlation")
	}
	if got := currentCorrelation.State(); got != observerelation.StateMissing {
		t.Fatalf("current replacement state = %q, want %q", got, observerelation.StateMissing)
	}
}

func TestRelationObserverSkipsHostFileWithoutSelectedClaudeCarrier(t *testing.T) {
	configRoot := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configRoot)
	writeTestFile(t, configRoot, "plugins/installed_plugins.json", `{malformed`)

	observations, err := relationhost.Observe(context.Background(), relationhost.Input{
		Lockfile:  lock.File{Version: lock.CurrentVersion},
		Selection: statusClaudeSelection(t, "codex"),
	})
	if err != nil {
		t.Fatalf("relationhost.Observe returned unrelated host-file error: %v", err)
	}
	if len(observations.AuthorityPaths()) != 0 {
		t.Fatalf("authority paths = %#v, want none without selected Claude carrier", observations.AuthorityPaths())
	}
}

func relationPendingForLockedRecord(
	t *testing.T,
	root string,
	record lock.LockedSubjectContract,
) durablecarrier.PendingCarrierInstall {
	t.Helper()
	identity, admitted, err := durablecarrier.ManagedCarrierIdentityFromLockedRecord(record)
	if err != nil || !admitted {
		t.Fatalf("derive carrier identity = (%#v, %t, %v)", identity, admitted, err)
	}
	request, err := lock.DelegatedOperationRequest(record, lock.OperationInstall)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := stateauthority.New(
		filepath.Join(root, ".daem", "state.json"),
		filepath.Join(root, "daem.toml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := durablecarrier.NewPendingCarrierInstall(owner, identity, request)
	if err != nil {
		t.Fatal(err)
	}
	return pending
}

func relationPendingWithHash(
	t *testing.T,
	pending durablecarrier.PendingCarrierInstall,
	hash string,
) durablecarrier.PendingCarrierInstall {
	t.Helper()
	request, err := realizationdelegate.NewRequest(
		pending.InstallRequest().RouteID(),
		pending.InstallRequest().ContractVersion(),
		hash,
	)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := durablecarrier.NewPendingCarrierInstall(
		pending.Owner(),
		pending.Identity(),
		request,
	)
	if err != nil {
		t.Fatal(err)
	}
	return changed
}

func relationClaimForPending(
	t *testing.T,
	pending durablecarrier.PendingCarrierInstall,
) durablecarrier.ManagedCarrierClaim {
	t.Helper()
	claim, err := durablecarrier.NewManagedCarrierClaim(
		pending.Owner(),
		pending.Identity(),
		pending.InstallRequest(),
		durablecarrier.ClaimProvenanceInstalledObserved,
	)
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func relationCarrierFactSnapshot(
	t *testing.T,
	pending []durablecarrier.PendingCarrierInstall,
	claims []durablecarrier.ManagedCarrierClaim,
) durable.Snapshot {
	t.Helper()
	snapshot, err := durable.NewSnapshot(durable.SnapshotInput{
		PendingCarrierInstalls: pending,
		ManagedCarrierClaims:   claims,
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
