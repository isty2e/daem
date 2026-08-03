package apply

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	targetpkg "github.com/isty2e/daem/internal/target"
)

func TestSelectedMCPEnvironmentSourceNamesAreTargetScopedDistinctAndStable(t *testing.T) {
	t.Parallel()

	environment := mcpPreflightEnvironment(
		t,
		[]targetpkg.Target{targetpkg.TargetClaudeCode, targetpkg.TargetCodex},
		mcpPreflightServer(t, "first", targetpkg.TargetClaudeCode, targetpkg.ScopeProject, map[string]string{
			"Z_CHILD": "HOST_SHARED",
			"A_CHILD": "HOST_ALPHA",
		}),
		mcpPreflightServer(t, "second", targetpkg.TargetClaudeCode, targetpkg.ScopeProject, map[string]string{
			"OTHER": "HOST_SHARED",
			"THIRD": "HOST_BETA",
		}),
		mcpPreflightServer(t, "unselected", targetpkg.TargetCodex, targetpkg.ScopeProject, map[string]string{
			"IGNORED": "UNSUPPORTED_UNSELECTED_SOURCE",
		}),
	)
	selection := mcpPreflightSelection(t, environment.Targets(), targetpkg.TargetClaudeCode)

	names, err := selectedMCPEnvironmentSourceNames(environment, selection)
	if err != nil {
		t.Fatalf("selectedMCPEnvironmentSourceNames returned error: %v", err)
	}
	want := []string{"HOST_ALPHA", "HOST_BETA", "HOST_SHARED"}
	if !slices.Equal(names, want) {
		t.Fatalf("source names = %#v, want %#v", names, want)
	}
	names[0] = "changed"
	repeated, err := selectedMCPEnvironmentSourceNames(environment, selection)
	if err != nil {
		t.Fatalf("repeated selectedMCPEnvironmentSourceNames returned error: %v", err)
	}
	if !slices.Equal(repeated, want) {
		t.Fatalf("repeated source names = %#v, want defensive result %#v", repeated, want)
	}
}

func TestPreflightMCPEnvironmentSourcesDistinguishesEmptyFromMissing(t *testing.T) {
	const sourceName = "DAEM_MENV_EMPTY_PRESENT"
	t.Setenv(sourceName, "")
	environment := mcpPreflightEnvironment(
		t,
		[]targetpkg.Target{targetpkg.TargetClaudeCode},
		mcpPreflightServer(t, "server", targetpkg.TargetClaudeCode, targetpkg.ScopeProject, map[string]string{
			"TOKEN": sourceName,
		}),
	)
	selection := mcpPreflightSelection(t, environment.Targets(), targetpkg.TargetClaudeCode)

	if err := preflightMCPEnvironmentSources(context.Background(), environment, selection, nil); err != nil {
		t.Fatalf("empty-but-present source was rejected: %v", err)
	}
	if err := preflightMCPEnvironmentSources(
		context.Background(),
		environment,
		selection,
		func(string) bool { return false },
	); err == nil {
		t.Fatal("missing source was accepted")
	}
}

func TestPreflightMCPEnvironmentSourcesBoundsSortedNameOnlyFailure(t *testing.T) {
	t.Parallel()

	env := make(map[string]string)
	for index := 0; index < maximumReportedMissingMCPEnvironmentSources+3; index++ {
		env[fmt.Sprintf("CHILD_%02d", index)] = fmt.Sprintf("SOURCE_%02d", index)
	}
	environment := mcpPreflightEnvironment(
		t,
		[]targetpkg.Target{targetpkg.TargetClaudeCode},
		mcpPreflightServer(t, "server", targetpkg.TargetClaudeCode, targetpkg.ScopeProject, env),
	)
	selection := mcpPreflightSelection(t, environment.Targets(), targetpkg.TargetClaudeCode)
	const secretSentinel = "MENV_SECRET_VALUE_MUST_NOT_APPEAR"

	err := preflightMCPEnvironmentSources(
		context.Background(),
		environment,
		selection,
		func(name string) bool {
			_ = secretSentinel
			return false
		},
	)
	var missing missingMCPEnvironmentSourcesError
	if !errors.As(err, &missing) {
		t.Fatalf("preflight error = %v, want missingMCPEnvironmentSourcesError", err)
	}
	if len(missing.names) != maximumReportedMissingMCPEnvironmentSources || missing.omitted != 3 {
		t.Fatalf("missing error = %#v", missing)
	}
	if !slices.IsSorted(missing.names) {
		t.Fatalf("reported names are not sorted: %#v", missing.names)
	}
	if strings.Contains(err.Error(), secretSentinel) ||
		strings.Contains(err.Error(), "SOURCE_08") ||
		!strings.Contains(err.Error(), "3 more omitted") {
		t.Fatalf("bounded error = %q", err)
	}
}

func TestPreflightMCPEnvironmentSourcesRejectsSelectedUnsupportedMappingBeforeLookup(t *testing.T) {
	t.Parallel()

	environment := mcpPreflightEnvironment(
		t,
		[]targetpkg.Target{targetpkg.TargetCodex},
		mcpPreflightServer(t, "server", targetpkg.TargetCodex, targetpkg.ScopeProject, map[string]string{
			"TOKEN": "HOST_TOKEN",
		}),
	)
	selection := mcpPreflightSelection(t, environment.Targets(), targetpkg.TargetCodex)
	lookups := 0

	err := preflightMCPEnvironmentSources(
		context.Background(),
		environment,
		selection,
		func(string) bool {
			lookups++
			return true
		},
	)
	if err == nil || !strings.Contains(err.Error(), "does not support environment references") {
		t.Fatalf("preflight error = %v, want unsupported mapping", err)
	}
	if lookups != 0 {
		t.Fatalf("presence lookups = %d, want none before capability rejection", lookups)
	}
}

func TestPreflightMCPEnvironmentSourcesHonorsCancellationBeforeLookup(t *testing.T) {
	t.Parallel()

	environment := mcpPreflightEnvironment(
		t,
		[]targetpkg.Target{targetpkg.TargetClaudeCode},
		mcpPreflightServer(t, "server", targetpkg.TargetClaudeCode, targetpkg.ScopeProject, map[string]string{
			"TOKEN": "HOST_TOKEN",
		}),
	)
	selection := mcpPreflightSelection(t, environment.Targets(), targetpkg.TargetClaudeCode)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	lookups := 0

	err := preflightMCPEnvironmentSources(ctx, environment, selection, func(string) bool {
		lookups++
		return true
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight error = %v, want context cancellation", err)
	}
	if lookups != 0 {
		t.Fatalf("presence lookups = %d, want none after prior cancellation", lookups)
	}
}

func TestPreflightMCPEnvironmentSourcesStopsAfterMidScanCancellation(t *testing.T) {
	t.Parallel()

	environment := mcpPreflightEnvironment(
		t,
		[]targetpkg.Target{targetpkg.TargetClaudeCode},
		mcpPreflightServer(t, "server", targetpkg.TargetClaudeCode, targetpkg.ScopeProject, map[string]string{
			"FIRST":  "A_SOURCE",
			"SECOND": "B_SOURCE",
		}),
	)
	selection := mcpPreflightSelection(t, environment.Targets(), targetpkg.TargetClaudeCode)
	ctx, cancel := context.WithCancel(context.Background())
	lookups := make([]string, 0)

	err := preflightMCPEnvironmentSources(ctx, environment, selection, func(name string) bool {
		lookups = append(lookups, name)
		cancel()
		return true
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight error = %v, want context cancellation", err)
	}
	if !slices.Equal(lookups, []string{"A_SOURCE"}) {
		t.Fatalf("presence lookups = %#v, want cancellation before second source", lookups)
	}
}
