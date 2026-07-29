package apply

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/isty2e/daem/internal/assurance/durable"
	durableattempt "github.com/isty2e/daem/internal/assurance/durable/attempt"
	durablecarrier "github.com/isty2e/daem/internal/assurance/durable/carrier"
	observeclaudeplugin "github.com/isty2e/daem/internal/assurance/observe/claudeplugin"
	observerelation "github.com/isty2e/daem/internal/assurance/observe/relation"
	"github.com/isty2e/daem/internal/effect/mutation/rootedpath"
	carrierclaimstore "github.com/isty2e/daem/internal/effect/storage/carrierclaim"
	daempaths "github.com/isty2e/daem/internal/paths"
	"github.com/isty2e/daem/internal/realization"
	lock "github.com/isty2e/daem/internal/realization/lock"
	"github.com/isty2e/daem/internal/subprocess"
	"github.com/isty2e/daem/internal/target"
	"github.com/isty2e/daem/internal/topology"
)

func hasRootedPathFailureKind(err error, kind rootedpath.FailureKind) bool {
	var failure *rootedpath.Failure
	return errors.As(err, &failure) && failure.Kind() == kind
}

func newApplyCarrierFixtureRoot(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if runtime.GOOS == "windows" {
		t.Setenv("LOCALAPPDATA", filepath.Join(root, "appdata", "local"))
	} else {
		t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	}
	return root
}

func isolatedApplyCarrierRegistryPath(t *testing.T, root string) string {
	t.Helper()

	resolved := applyTestPaths(t, root)
	if resolved.CarrierClaimRegistryPath != filepath.Join(resolved.DataDir, "carriers", "claims.json") {
		t.Fatalf(
			"carrier claim registry = %q, want path correlated to isolated data dir %q",
			resolved.CarrierClaimRegistryPath,
			resolved.DataDir,
		)
	}
	relative, err := filepath.Rel(root, resolved.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatalf("relativize carrier claim registry to fixture root: %v", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf(
			"carrier claim registry = %q, want path inside fixture root %q",
			resolved.CarrierClaimRegistryPath,
			root,
		)
	}
	return resolved.CarrierClaimRegistryPath
}

func writeApplyClaudePluginCarrierCommandFixture(
	t *testing.T,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	return writeApplyClaudePluginCarrierCommandFixtureForScope(t, target.ScopeProject)
}

func writeApplyClaudePluginCarrierCommandFixtureForScope(
	t *testing.T,
	scope target.Scope,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyFile(t, manifestPath, `
version = 1
targets = ["claude-code"]
`)
	locked, subject := applyExtensionDerivedClaudePluginCarrierLockfileForScope(t, scope)
	writeApplyLockfile(t, lockfilePath, locked)
	missingInventory := applyClaudePluginCarrierInventory(t, observeclaudeplugin.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	return root, manifestPath, lockfilePath, applyClaudeObservationBatch(t, locked, subject, missingInventory), locked, subject
}

func writeApplyCodexPluginCarrierCommandFixture(
	t *testing.T,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyFile(t, manifestPath, `
version = 1
targets = ["codex"]
`)
	locked, subject := applyExtensionDerivedCodexPluginCarrierLockfile(t)
	writeApplyLockfile(t, lockfilePath, locked)
	missingInventory := applyRelationInventory(t, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	return root, manifestPath, lockfilePath, applyRelationObservationBatch(t, locked, subject, missingInventory), locked, subject
}

func writeApplyOpenCodePluginCarrierCommandFixture(
	t *testing.T,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	return writeApplyOpenCodePluginCarrierCommandFixtureForScope(t, target.ScopeGlobal)
}

func writeApplyOpenCodePluginCarrierCommandFixtureForScope(
	t *testing.T,
	scope target.Scope,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyFile(t, manifestPath, `
version = 1
targets = ["opencode"]
`)
	locked, subject := applyExtensionDerivedOpenCodePluginCarrierLockfileForScope(t, scope)
	writeApplyLockfile(t, lockfilePath, locked)
	missingInventory := applyRelationInventory(t, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	return root, manifestPath, lockfilePath, applyRelationObservationBatch(t, locked, subject, missingInventory), locked, subject
}

func writeApplyPiPackageCarrierCommandFixture(
	t *testing.T,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	return writeApplyPiPackageCarrierCommandFixtureForScope(t, target.ScopeProject)
}

func writeApplyPiPackageCarrierCommandFixtureForScope(
	t *testing.T,
	scope target.Scope,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyFile(t, manifestPath, `
version = 1
targets = ["pi"]
`)
	locked, subject := applyExtensionDerivedPiPackageCarrierLockfileForScope(t, scope)
	writeApplyLockfile(t, lockfilePath, locked)
	missingInventory := applyRelationInventory(t, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	return root, manifestPath, lockfilePath, applyRelationObservationBatch(t, locked, subject, missingInventory), locked, subject
}

func writeApplyAntigravityCLIPluginCarrierCommandFixture(
	t *testing.T,
) (string, string, string, observerelation.Batch, lock.File, realization.DelegatedRelation) {
	t.Helper()
	root := newApplyCarrierFixtureRoot(t)
	manifestPath := filepath.Join(root, "daem.toml")
	lockfilePath := filepath.Join(root, "daem.lock.toml")
	writeApplyFile(t, manifestPath, `
version = 1
targets = ["antigravity-cli"]
`)
	locked, subject := applyExtensionDerivedAntigravityCLIPluginCarrierLockfile(t)
	writeApplyLockfile(t, lockfilePath, locked)
	missingInventory := applyRelationInventory(t, observerelation.InventorySpec{
		Availability: observerelation.InventorySupported,
		Freshness:    observerelation.EvidenceFresh,
	})
	return root, manifestPath, lockfilePath, applyRelationObservationBatch(t, locked, subject, missingInventory), locked, subject
}

func fixedApplyHostRouteClock() time.Time {
	return time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC)
}

func assertApplyClaudeHostRouteCommandRequest(t *testing.T, root string, requests []subprocess.CommandRequest, hostScope string) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one", requests)
	}
	request := requests[0]
	if request.Command != "claude" ||
		!slices.Equal(request.Args, []string{"plugin", "install", "context7@market", "--scope", hostScope}) ||
		request.WorkDir != root {
		t.Fatalf("host route request = %#v, want claude plugin install context7@market --scope %s in %q", request, hostScope, root)
	}
}

func assertApplyCodexHostRouteCommandRequest(t *testing.T, root string, requests []subprocess.CommandRequest) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one", requests)
	}
	request := requests[0]
	if request.Command != "codex" ||
		!slices.Equal(request.Args, []string{"plugin", "add", "documents@openai-primary-runtime", "--json"}) ||
		request.WorkDir != root {
		t.Fatalf("host route request = %#v, want codex plugin add documents@openai-primary-runtime --json in %q", request, root)
	}
}

func assertApplyOpenCodeHostRouteCommandRequest(t *testing.T, root string, requests []subprocess.CommandRequest) {
	t.Helper()
	assertApplyOpenCodeHostRouteCommandRequestForScope(t, root, requests, target.ScopeGlobal)
}

func assertApplyOpenCodeHostRouteCommandRequestForScope(
	t *testing.T,
	root string,
	requests []subprocess.CommandRequest,
	scope target.Scope,
) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one", requests)
	}
	wantArgs := []string{"plugin", "@acme/opencode-formatter"}
	if scope == target.ScopeGlobal {
		wantArgs = append(wantArgs, "--global")
	}
	request := requests[0]
	if request.Command != "opencode" ||
		!slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != root {
		t.Fatalf("host route request = %#v, want opencode %v in %q", request, wantArgs, root)
	}
}

func assertApplyPiHostRouteCommandRequest(t *testing.T, root string, requests []subprocess.CommandRequest) {
	t.Helper()
	assertApplyPiHostRouteCommandRequestForScope(t, root, requests, target.ScopeProject)
}

func assertApplyPiHostRouteCommandRequestForScope(
	t *testing.T,
	root string,
	requests []subprocess.CommandRequest,
	scope target.Scope,
) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one", requests)
	}
	wantArgs := []string{"install", "github:acme/pi-tools"}
	if scope == target.ScopeProject {
		wantArgs = append(wantArgs, "-l")
	}
	request := requests[0]
	if request.Command != "pi" ||
		!slices.Equal(request.Args, wantArgs) ||
		request.WorkDir != root {
		t.Fatalf("host route request = %#v, want pi %v in %q", request, wantArgs, root)
	}
}

func assertApplyAntigravityCLIHostRouteCommandRequest(t *testing.T, root string, requests []subprocess.CommandRequest) {
	t.Helper()
	if len(requests) != 1 {
		t.Fatalf("host route requests = %#v, want one", requests)
	}
	request := requests[0]
	if request.Command != "agy" ||
		!slices.Equal(request.Args, []string{"plugin", "install", "modern-web-guidance@google"}) ||
		request.WorkDir != root {
		t.Fatalf("host route request = %#v, want agy plugin install modern-web-guidance@google in %q", request, root)
	}
}

func assertApplyHostRouteAttempt(
	t *testing.T,
	attempts []durableattempt.HostRouteAttempt,
	subject topology.SubjectID,
	wantClass durableattempt.HostRouteResultClass,
	wantReason durableattempt.HostRouteResultReason,
	wantObserved bool,
) {
	t.Helper()
	assertApplyHostRouteAttemptFor(t, attempts, subject, "claude-code", "project", "claude-code.plugin-carrier.install", wantClass, wantReason, wantObserved)
}

func assertApplyHostRouteAttemptFor(
	t *testing.T,
	attempts []durableattempt.HostRouteAttempt,
	subject topology.SubjectID,
	wantTarget string,
	wantScope string,
	wantRouteID string,
	wantClass durableattempt.HostRouteResultClass,
	wantReason durableattempt.HostRouteResultReason,
	wantObserved bool,
) {
	t.Helper()
	if len(attempts) != 1 {
		t.Fatalf("HostRouteAttempts = %#v, want one", attempts)
	}
	attempt := attempts[0]
	if attempt.Subject() != subject ||
		string(attempt.Target()) != wantTarget ||
		string(attempt.Scope()) != wantScope ||
		attempt.RouteID() != wantRouteID ||
		!strings.HasPrefix(attempt.RouteRequestHash(), "sha256:") ||
		attempt.ResultClass() != wantClass ||
		attempt.Reason() != wantReason ||
		!attempt.AttemptObserved() ||
		(attempt.ObservationSummary() == observerelation.ObservationPresent) != wantObserved {
		t.Fatalf("HostRouteAttempt = %#v, want class=%q reason=%q observed=%t without skip authority", attempt, wantClass, wantReason, wantObserved)
	}
	if !attempt.ObservedAt().Equal(fixedApplyHostRouteClock()) {
		t.Fatalf("ObservedAt = %q, want fixed clock", attempt.ObservedAt())
	}
}

func assertApplyProjectManagedCarrierClaim(
	t *testing.T,
	state durable.Snapshot,
	record lock.LockedSubjectContract,
) {
	t.Helper()
	claims := state.ManagedCarrierClaims()
	if len(claims) != 1 {
		t.Fatalf("ManagedCarrierClaims = %#v, want one", claims)
	}
	if !claims[0].MatchesLockedRecord(record) {
		t.Fatalf("managed carrier claim = %#v, want exact current lock identity", claims[0])
	}
}

func assertApplyPendingCarrierInstallRows(
	t *testing.T,
	pending []durablecarrier.PendingCarrierInstall,
	record lock.LockedSubjectContract,
) {
	t.Helper()
	if len(pending) != 1 {
		t.Fatalf("PendingCarrierInstalls = %#v, want one", pending)
	}
	if !pending[0].MatchesLockedRecord(record) {
		t.Fatalf("pending carrier install = %#v, want exact current lock identity", pending[0])
	}
}

func assertApplyPendingCarrierInstall(
	t *testing.T,
	state durable.Snapshot,
	record lock.LockedSubjectContract,
) {
	t.Helper()
	assertApplyPendingCarrierInstallRows(t, state.PendingCarrierInstalls(), record)
}

func assertApplyNoCarrierFact(t *testing.T, state durable.Snapshot, subject topology.SubjectID) {
	t.Helper()
	for _, pending := range state.PendingCarrierInstalls() {
		if pending.Identity().RelationSubject() == subject {
			t.Fatalf("unexpected pending carrier install for %s/%s/%s: %#v", subject.Kind(), subject.Namespace(), subject.Key(), pending)
		}
	}
	for _, claim := range state.ManagedCarrierClaims() {
		if claim.Identity().RelationSubject() == subject {
			t.Fatalf("unexpected managed carrier claim for %s/%s/%s: %#v", subject.Kind(), subject.Namespace(), subject.Key(), claim)
		}
	}
}

func assertApplyGlobalManagedCarrierClaim(
	t *testing.T,
	root string,
	record lock.LockedSubjectContract,
) {
	t.Helper()
	resolved, err := daempaths.Resolve(filepath.Join(root, "daem.toml"))
	if err != nil {
		t.Fatalf("resolve carrier claim registry: %v", err)
	}
	store, err := carrierclaimstore.New(resolved.CarrierClaimRegistryPath)
	if err != nil {
		t.Fatalf("open carrier claim registry: %v", err)
	}
	registry, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("load carrier claim registry: %v", err)
	}
	claims := registry.Claims()
	if len(claims) != 1 || !claims[0].MatchesLockedRecord(record) {
		t.Fatalf("global carrier claims = %#v, want one exact current lock claim", claims)
	}
}

func applyPriorHostRouteAttemptRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
) durableattempt.HostRouteAttempt {
	t.Helper()
	realization, ok := record.Realization()
	if !ok {
		t.Fatal("test fixture missing delegated relation realization")
	}
	relation, ok := realization.DelegatedRelation()
	if !ok {
		t.Fatal("test fixture realization is not a delegated relation")
	}
	attempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          record.SubjectID(),
		Target:           relation.Target(),
		Scope:            relation.Scope(),
		Operation:        lock.OperationInstall,
		RouteID:          relation.RouteID(),
		RouteRequestHash: relation.CanonicalRequestHash(),
		ObservedAt:       time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
		ResultClass:      durableattempt.HostRouteResultAttemptedObservedPresent,
		Reason:           durableattempt.HostRouteReasonObservedPresent,
		AttemptObserved:  true,
		Observation:      observerelation.ObservationPresent,
		Postcondition:    observerelation.PostconditionObserved,
	})
	if err != nil {
		t.Fatalf("build prior host route attempt: %v", err)
	}
	return attempt
}

func applyPriorAttemptedUnverifiedHostRouteAttemptRecord(
	t *testing.T,
	record lock.LockedSubjectContract,
) durableattempt.HostRouteAttempt {
	t.Helper()
	prior := applyPriorHostRouteAttemptRecord(t, record)
	attempt, err := durableattempt.NewHostRouteAttempt(durableattempt.HostRouteAttemptInput{
		Subject:          prior.Subject(),
		Target:           prior.Target(),
		Scope:            prior.Scope(),
		Operation:        prior.Operation(),
		RouteID:          prior.RouteID(),
		RouteRequestHash: prior.RouteRequestHash(),
		ObservedAt:       prior.ObservedAt(),
		ResultClass:      durableattempt.HostRouteResultAttemptedUnverified,
		Reason:           durableattempt.HostRouteReasonObservationUnavailable,
		AttemptObserved:  true,
		Observation:      observerelation.ObservationNotObserved,
		Postcondition:    observerelation.PostconditionUnknown,
	})
	if err != nil {
		t.Fatalf("build prior unverified host route attempt: %v", err)
	}
	return attempt
}

func intPtr(value int) *int {
	return &value
}
