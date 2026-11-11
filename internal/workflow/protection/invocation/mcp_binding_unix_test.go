//go:build darwin || linux

package invocation

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/isty2e/daem/internal/effect/physicalwork"
	"github.com/isty2e/daem/internal/output"
	"github.com/isty2e/daem/internal/output/ownership"
	"github.com/isty2e/daem/internal/realization/aggregate"
	"github.com/isty2e/daem/internal/subprocess"
)

func TestProspectiveInvocationProtectsBoundMCPInItsPhysicalCohort(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	literal := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(literal, []byte("unrelated literal MCP file\n"), 0o660); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(literal, 0o660); err != nil {
		t.Fatal(err)
	}
	fixture, input, bind := invocationFixture(t)
	physical := fixture.Protection.Documents()[1].PathAuthority()
	destination, err := output.Parse(aggregate.ClaudeGlobalMCPConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := ownership.NewDocumentBinding(fixture.Owner, destination, physical)
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	value, _ := ownership.PresentDocumentBinding(binding)
	current, err = current.ApplyDocumentBinding(fixture.Owner.Key(), destination, ownership.NoDocumentBinding(), value)
	if err != nil {
		t.Fatal(err)
	}
	publishLayoutFixtureRegistry(t, fixture, current)
	layout, budget := prospectiveLayoutFixture(t, fixture, input, false, nil)
	if err := layout.ReserveWork(budget, physicalwork.InvocationWork{}); err != nil {
		t.Fatal(err)
	}
	attached, err := layout.AttachProtection(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cohort := false
	for _, document := range attached.Protection.Documents() {
		if document.PathAuthority().Equal(physical) {
			cohort = len(document.Bindings()) == 2
		}
	}
	if !cohort {
		t.Fatal("attachment lost the co-resident MCP binding")
	}
	beforeCommand, err := fixture.Store.LoadProtectionRegistry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	result, err := ExecuteInvocation(t.Context(), attached, subprocess.CommandExecutor{}, subprocess.CommandAttemptRequest{
		Command: "/bin/sh", Args: []string{"-c", `/bin/chmod 666 "$1"`, "protected", fixture.Documents[1]},
	}, bind, nil)
	if !errors.Is(err, ErrInvocationPermissionsExpanded) {
		t.Fatal("bound physical MCP permission expansion was not contained", err)
	}
	assertInvocationSettled(t, fixture, result)
	if actual, err := fixture.Store.LoadProtectionRegistry(t.Context()); err != nil || !actual.Equal(beforeCommand) {
		t.Fatal("containment changed successful bindings or claims", err)
	}
	if info, err := os.Stat(fixture.Documents[1]); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("physical cohort permission ceiling was not restored", err)
	}
	if info, err := os.Stat(literal); err != nil || info.Mode().Perm() != 0o660 {
		t.Fatal("protection changed the unrelated literal path", err)
	}
	if content, err := os.ReadFile(literal); err != nil || string(content) != "unrelated literal MCP file\n" {
		t.Fatal("protection changed unrelated literal content", err)
	}
}
