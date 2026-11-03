//go:build darwin || linux

package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/isty2e/daem/internal/assurance/pathauthority"
)

func TestPreparedVisibilityAcceptsCreationWithinItsSeparateProjection(t *testing.T) {
	prepared, leases, path := preparedVisibilityFixture(t)
	projection := preparedBudget()
	if err := prepared.ReserveVisibilityAcceptance(projection); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("created"), 0o600); err != nil {
		t.Fatal(err)
	}
	allocation := &preparedWorkBudget{pathLimit: projection.paths, entryLimit: projection.entries, byteLimit: projection.bytes}
	if accepted, err := prepared.AcceptVisibilityChanges(t.Context(), allocation); err != nil || !accepted {
		t.Fatalf("prepared acceptance failed: %v actual=%+v projection=%+v", err, allocation, projection)
	}
	physical, err := ObservePersistedDirectoryEntryAuthority(path)
	if err != nil {
		t.Fatal(err)
	}
	check := preparedBudget()
	if err := prepared.ReserveCheck(check); err != nil {
		t.Fatal(err)
	}
	if matched, err := prepared.MatchCurrentExclusiveDocuments(t.Context(), []pathauthority.Exact{physical.Exact()},
		&preparedWorkBudget{pathLimit: check.paths, entryLimit: check.entries, byteLimit: check.bytes}); err != nil || !matched {
		t.Fatal("accepted path lost its original requested coverage", err)
	}
	leases.Release()
	if err := prepared.ReserveVisibilityAcceptance(preparedBudget()); err == nil {
		t.Fatal("released leases projected more visibility work")
	}
	if accepted, err := prepared.AcceptVisibilityChanges(t.Context(), preparedBudget()); err == nil || accepted {
		t.Fatal("prepared acceptance extended released lease lifetime")
	}
}

func TestPreparedVisibilityRefusalDoesNotPartiallyRebindDomains(t *testing.T) {
	for _, failure := range []string{"cancelled", "no allocation", "post validation"} {
		t.Run(failure, func(t *testing.T) {
			prepared, leases, path := preparedVisibilityFixture(t)
			if err := os.Mkdir(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("created"), 0o600); err != nil {
				t.Fatal(err)
			}
			before := append([]Domain(nil), leases.domains...)
			ctx, budget := t.Context(), preparedBudget()
			var cause error
			var namespace *postValidationFailureNamespace
			switch failure {
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				cause = context.Canceled
			case "no allocation":
				budget = &preparedWorkBudget{}
			case "post validation":
				cause = errors.New("post validation failed")
				namespace = &postValidationFailureNamespace{
					preparedLeaseNamespace: leases.namespace,
					bounded:                leases.namespace.(boundedLeaseNamespace), cause: cause,
				}
				leases.namespace = namespace
			}
			accepted, err := prepared.AcceptVisibilityChanges(ctx, budget)
			if err == nil || accepted || !reflect.DeepEqual(before, leases.domains) {
				t.Fatal("failed visibility acceptance changed held domains", err)
			}
			if cause != nil && !errors.Is(err, cause) {
				t.Fatal("visibility refusal lost its cause", err)
			}
			if namespace != nil && namespace.calls != 2 {
				t.Fatal("failure did not occur at the bounded post-validation edge")
			}
		})
	}
}

func TestPreparedVisibilityProjectionDoesNotFollowChangedAliases(t *testing.T) {
	root := t.TempDir()
	for _, directory := range []string{"first", "second"} {
		if err := os.Mkdir(filepath.Join(root, directory), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, directory, "config"), []byte(directory), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	control, alias := filepath.Join(root, "state.json"), filepath.Join(root, "alias")
	if err := os.WriteFile(control, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("first", alias); err != nil {
		t.Fatal(err)
	}
	prepared, leases := preparedDocumentCoverageFixture(t, []string{control}, []PhysicalAuthorityRequest{{Path: filepath.Join(alias, "config"), Target: "pi", Scope: "project"}})
	before := append([]Domain(nil), leases.domains...)
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("second", alias); err != nil {
		t.Fatal(err)
	}
	if err := prepared.ReserveVisibilityAcceptance(preparedBudget()); err != nil {
		t.Fatal("pure projection observed a changed alias", err)
	}
	if accepted, err := prepared.AcceptVisibilityChanges(t.Context(), preparedBudget()); err == nil || accepted || !reflect.DeepEqual(before, leases.domains) {
		t.Fatal("visibility acceptance followed an unprepared alias", err)
	}
}

func preparedVisibilityFixture(t *testing.T) (PreparedExclusiveRequests, *LeaseSet, string) {
	t.Helper()
	root := t.TempDir()
	control := filepath.Join(root, "state.json")
	if err := os.WriteFile(control, []byte("state"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "new", "설정.json")
	prepared, leases := preparedDocumentCoverageFixture(t, []string{control}, []PhysicalAuthorityRequest{
		{Path: path, Target: "pi", Scope: "project"},
		{Path: filepath.Join(root, "later", "다른.json"), Target: "codex", Scope: "project"},
	})
	return prepared, leases, path
}

type postValidationFailureNamespace struct {
	preparedLeaseNamespace
	bounded boundedLeaseNamespace
	cause   error
	calls   int
}

func (namespace *postValidationFailureNamespace) validateCurrentBounded(ctx context.Context, depth int, budget PhysicalObservationBudget, observe pathIdentityObserver) error {
	namespace.calls++
	if namespace.calls == 2 {
		return namespace.cause
	}
	return namespace.bounded.validateCurrentBounded(ctx, depth, budget, observe)
}
