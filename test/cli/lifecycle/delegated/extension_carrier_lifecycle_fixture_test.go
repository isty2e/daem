package cli_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/isty2e/daem/internal/topology"
	"github.com/isty2e/daem/test/testkit/clijson"
)

type hostSourceHostRouteAttemptExpectation struct {
	namespace   string
	name        string
	target      string
	scope       string
	routeID     string
	resultClass string
	reason      string
}

func assertCLIHostSourceHostRouteAttemptJSON(
	t *testing.T,
	attempts []clijson.HostRouteAttempt,
	want hostSourceHostRouteAttemptExpectation,
) {
	t.Helper()
	if len(attempts) != 1 {
		t.Fatalf("host_route_attempts = %#v, want one", attempts)
	}
	attempt := attempts[0]
	if attempt.EvidenceKind != "host_route_attempt_diagnostics" ||
		attempt.Authority != "history_only" ||
		attempt.Subject.Kind != string(topology.SubjectHostRelation) ||
		attempt.Subject.Namespace != want.namespace ||
		attempt.Subject.Name != want.name ||
		attempt.Target != want.target ||
		attempt.Scope != want.scope ||
		attempt.RouteID != want.routeID ||
		!strings.HasPrefix(attempt.RouteRequestHash, "sha256:") ||
		attempt.ResultClass != want.resultClass ||
		attempt.Reason != want.reason ||
		!attempt.AttemptObserved ||
		attempt.GrantsApplySkipAuthority {
		t.Fatalf("host_route_attempt = %#v, want %#v history-only diagnostic", attempt, want)
	}
	if !slices.Contains(attempt.NonClaims, "future_skip_authority") ||
		!slices.Contains(attempt.NonClaims, "package_cache_convergence") ||
		!slices.Contains(attempt.NonClaims, "runtime_readiness") {
		t.Fatalf("non_claims = %#v, want retained-effect non-claims", attempt.NonClaims)
	}
}
