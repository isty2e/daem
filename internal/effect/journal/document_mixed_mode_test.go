package journal_test

import (
	"testing"

	"github.com/isty2e/daem/internal/effect/journal"
	"github.com/isty2e/daem/internal/effect/journal/recovery"
	"github.com/isty2e/daem/internal/encoding/configdocument"
	"github.com/isty2e/daem/internal/realization/aggregate"
	aggregatecodec "github.com/isty2e/daem/internal/realization/aggregate/codec"
	"github.com/isty2e/daem/internal/realization/aggregate/compose"
)

func mixedModeParticipants(t *testing.T) []compose.Participant {
	t.Helper()
	participants := restorationMCPParticipants(t)
	var required []aggregate.CodecContractID
	for _, participant := range participants {
		required = append(required, participant.Selection().CodecContractID())
	}
	limits := configdocument.Limits{MaximumBytes: 4096, MaximumDepth: 32}
	catalog, err := aggregatecodec.ForOperation(t.Context(), required, limits, limits)
	if err != nil {
		t.Fatal(err)
	}
	for index, participant := range participants {
		participants[index], err = journal.NativeJournalParticipant(participant.Selection(), catalog)
		if err != nil {
			t.Fatal(err)
		}
	}
	return participants
}

func TestSelectedMCPModeTransitionRequiresCompletePhaseAndRestoresBefore(t *testing.T) {
	beforeMode, expectedMode := recovery.PermissionMode(0o644), recovery.PermissionMode(0o600)
	before := "model='balanced'\n"
	expected := "model='fast'\n[mcp_servers.search]\ncommand='node'\n"
	comparison, err := journal.NewSelectedDocumentComparison(t.Context(), comparisonInput(before, expected, &beforeMode, &expectedMode), mixedModeParticipants(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name             string
		content          string
		mode             recovery.PermissionMode
		before, expected bool
	}{
		{name: "before", content: before, mode: beforeMode, before: true},
		{name: "expected", content: expected, mode: expectedMode, expected: true},
		{name: "before values expected mode", content: before, mode: expectedMode},
		{name: "expected values before mode", content: expected, mode: beforeMode},
	} {
		t.Run(test.name, func(t *testing.T) {
			current := aggregate.ExistingDocument([]byte("# fresh user data\nforeign=true\n" + test.content))
			match, err := comparison.Match(t.Context(), current, &test.mode)
			if err != nil || !match.Recoverable() || match.MatchesBefore() != test.before || match.MatchesExpected() != test.expected {
				t.Fatalf("content/mode phase was lost: %+v, %v", match, err)
			}
			restored, err := comparison.RestoreBefore(t.Context(), current, &test.mode)
			if err != nil {
				t.Fatal(err)
			}
			if mode, present := restored.RegularFileMode(); !present || mode != beforeMode {
				t.Fatal("selected rollback did not restore the original file mode")
			}
			clean, err := comparison.Match(t.Context(), restored.Document(), &beforeMode)
			if err != nil || !clean.MatchesBefore() {
				t.Fatal("selected rollback did not settle Before", err)
			}
			again, err := comparison.RestoreBefore(t.Context(), restored.Document(), &beforeMode)
			if err != nil || again.Changed() {
				t.Fatal("mode rollback was not idempotent", err)
			}
		})
	}
	thirdMode := recovery.PermissionMode(0o640)
	if restored, err := comparison.RestoreBefore(t.Context(), aggregate.ExistingDocument([]byte(expected)), &thirdMode); err == nil || restored.Validate() == nil {
		t.Fatal("third file mode returned a usable restoration")
	}
}

func TestSelectedKeyOnlyCannotAuthorizeAnExistingModeTransition(t *testing.T) {
	beforeMode, expectedMode := recovery.PermissionMode(0o644), recovery.PermissionMode(0o600)
	comparison, err := journal.NewSelectedDocumentComparison(t.Context(), comparisonInput(`{"model":"balanced"}`, `{"model":"fast"}`, &beforeMode, &expectedMode), []compose.Participant{comparisonParticipant(t, "model")}, nil)
	if err == nil || comparison.Validate() == nil {
		t.Fatal("native keys acquired document mode authority")
	}
}
